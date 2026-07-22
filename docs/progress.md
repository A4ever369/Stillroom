# Stillroom 进度文档

> 维护约定:每完成一个里程碑或做出一个影响方向的决策,在这里追加一条(带日期)。
> 本文档是项目的"进度事实源";设计原理见 `docs/design-v2.md`。

## 状态总览

| 里程碑 | 内容 | 状态 |
| --- | --- | --- |
| M0 骨架 | ir / redact / adapter / distill / materialize / CLI / 插件,全部带单测 | ✅ 2026-07-19 |
| M1 自食 | session 自动发现、台账、近重复防护、doctor;**真实 session 蒸馏质量验证** | 🚧 首次真实验证已跑通(成色高),prompt 调优循环启动 |
| M2 开源发布 | repo 公开、发射动作(§14)、第一批外部用户 | ⬜ |
| M3 融合验证 | 双人 merge 三条路径、任务级评估 | ⬜ |
| M4 服务端(Phase 2) | 证据库、回放、检索、MCP 面;商业化启动 | ⬜ |

## 变更日志

### 2026-07-22 — `still status --json`(结构化状态,给 CI/工具消费)

`cmdStatus` 重构为先算一个 `statusReport` 结构、再渲染文本或 JSON——两种输出同源,
永不打架。JSON 含:facts(total/active/bad)、playbooks(total/bad)、pending_sessions、
discovery(claude_code/codex 各自数量)、bad_files(排序、永不 null)、
materialized_up_to_date(顺带把漂移信号也带进 status 文本)。文本格式保持不变(不破坏
现有测试/smoke)。测试:黑盒解析 JSON 断言字段 + 坏文件同时进 count 和 bad_files。

### 2026-07-22 — materialized.md 漂移检测(`--check` + doctor)

真实失败模式:有人手改 fact 或 merge 落地改了 `facts/`,但忘了 `still materialize`,
提交的 `materialized.md` 就**过时**——而它正是每个队友 agent 加载的团队上下文。

- `materialize.Run` 拆出纯函数 `Render`(只算字节、不落盘;确定性渲染正好让「重算 ==
  磁盘」成为漂移判据)。
- `still materialize --check`:重算并与磁盘比,过时则 exit 1 提示「run `still materialize`
  and commit」,可作 CI 门禁(README 命令表已标 CI-friendly)。
- `still doctor` 新增一项「materialized.md is up to date」。
- 测试:materialize 单测(Render 与 Run 字节一致且不落盘)+ cmd 黑盒(distill 后 check
  通过 → 手加 fact → check 失败且 doctor 报警 → 重 materialize → 再通过),跑的是真二进制。

**为什么不做 Codex hook**:探过本机 `~/.codex`,其 `notify`/hooks 机制的载荷格式无法确认
是否携带 rollout 路径,凭空猜违背「对着现实建」。且 Codex 自动发现已经能用,hook 只是
新鲜度优化,非必需——故不做投机实现。

### 2026-07-22 — L4 场景矩阵补全:smoke.sh 从单条 happy path 扩成 6 场景

`scripts/smoke.sh` 重写成**隔离场景矩阵**(纯 bash + fake claude,零 token),每个
场景一个独立世界(临时 repo + `CLAUDE_CONFIG_DIR`/`CODEX_HOME` + fake claude),失败
局部化。测的是**编译出的真二进制**——用户实际那条链路(shell 集成、插件 hook、
真文件系统),是 Go 黑盒测不到的一层。六场景全绿:

1. cold-start:init→doctor→自动发现→distill(含脱敏断言)→materialize
2. **hook 入队**:`still hook session-end` 读 `{transcript_path,cwd}` 入队 → 队列文件 →
   `status` pending 1 → distill 消费 → 队列清空(转录刻意放发现目录外,唯一入口是队列)
3. 幂等 + `--force`
4. **Codex 发现**:CODEX_HOME 放 cwd 匹配本 repo 的 rollout → distill 端到端发现并蒸馏,
   多工具接线走通真二进制
5. 融合(仍在 `fusion_test.go`,Go 黑盒)
6. **升级路径**:老布局 → `init` 就地升级补齐 gitattributes/gitkeep/gitignore 且不丢数据
7. review diff

顺手抓到一个纯 bash 坑:`printf "$fmt"` 当 `$fmt` 以 `---` 开头会被当成选项,改用
heredoc。**至此 L0–L5 六层全部有可执行证据,测试方案 100% 落地。**

### 2026-07-22 — review 寄生落地:PR 自动评论知识 diff(§13 最后一块)

- **`internal/review`(新)**:纯函数,把两个知识快照(base/head)按 **fact id**
  做**语义** diff(不是文本 diff),分类 新增/更新/移除,fact 观察前进的标成
  supersession。`Markdown()` 确定性排序输出,顶部带隐形锚 `<!-- stillroom-knowledge-diff -->`
  让 bot 就地更新同一条评论而非刷屏。100% 覆盖。
- **`still review --base DIR [--head DIR]`(新命令)**:head 默认本仓库,base 默认空
  (首次采纳=全部新增)。永不 fail build,坏文件跳过仍 exit 0。
- **`.github/workflows/knowledge-diff.yml`(新)**:PR 触及 `.team-context/facts|playbooks`
  时触发;`git worktree` 拉出 base 分支快照 → `still review` → 用 **first-party**
  `actions/github-script` 按锚查找并就地更新 PR 评论。零第三方 action,契合零依赖气质。
- 测试:review 包单测(分类/幂等重写不算 diff/playbook/确定性/全 section 渲染)+
  cmd 黑盒(`--base/--head` 两 dir 直跑,断言锚 + 新 fact)。

**至此 §13 的 review 寄生闭环成形**:知识变更不再需要独立评审面,搭着团队正常
PR review 一起看。

### 2026-07-22 — Codex adapter 落地(第二个源工具)+ digest 类型下沉为 tool-agnostic

M2 路线上的多工具支持迈出第一步:接入 **OpenAI Codex CLI**。

- **`internal/session`(新)**:把 `Digest`/`Meta` 及渲染工具(`Clip`/`WriteTurn`/
  `CompactJSON`/`CompactAny`)从 `claudecode` 下沉成 tool-agnostic 类型。distill 管线
  现在只认 `session.Digest`,加一个工具 = 加一个 adapter,下游零改动。`Meta` 新增
  `Tool` 字段(`claude-code`/`codex`),决定 fact 上 source ref 的 scheme。
- **`internal/adapter/codex`(新)**:对着**本机真实 rollout 文件**逆出格式
  (`~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`,每行 `{timestamp,type,payload}`)。
  `session_meta`→cwd/session_id;`response_item` 的 message/function_call/
  function_call_output 进 digest,reasoning 丢弃(同 claudecode 丢 thinking)。
  **实测**:一个真实 904-turn session 正确解析出 tool/turns/cwd/session/毫秒级
  LastActivity。
- **发现差异**:Codex 不像 Claude Code 用 encoded-cwd 目录名,而是按日期存、cwd 写在
  文件里 —— 所以 `codex.Discover` 读每个 rollout 的首个 `session_meta` 行取 cwd 匹配。
- **质量决策**:Codex 的 `developer` 角色消息是注入的环境/审批/base-instructions
  框架(能把 digest 顶到 200KB 上限),**丢弃**,只留 user/assistant —— 与 claudecode
  只渲染 user/assistant 一致。
- **接线**:`still distill` 现在同时发现 Claude Code + Codex session;队列路径按
  `IsRollout`(basename `rollout-*.jsonl`)分派到对应 adapter;`doctor` 分别报两者数量。
- **测试**:codex 单测(格式/时间/mtime 回落/坏行容错/IsRollout/Discover 按 cwd 匹配)
  + fuzz target(接入 nightly)。codex 87.9%、session 89.7%,全绿。

### 2026-07-22 — 首次真实 session 蒸馏验证 + 证据驱动的第一次 prompt 调优

拿本仓库自己的开发 session(441 turns,即建整套测试体系那段)跑
`still distill --dry-run`,产出 **15 facts + 1 playbook**。成色评估(L5 三轴人工目测):

- **召回近乎满分**:这段 session 的每个真实决策/bug 都抓到了,且细节精确(连
  「fake claude 要 `#!/bin/sh` + 绝对 `/bin/cat`,因为测试把 PATH 清空」这种子细节都对)。
- **精度高**:无幻觉配置,全部落在真实发生的事上。
- **playbook `diagnose-fuzz-stall` 质量优秀、可迁移**:把这次排查 fuzz 假挂起的
  方法论完整固化成可复用配方。

**但炸出一个明确的 prompt 缺陷——过度捕获会话/工具元观察**。两条噪声 fact:
`distill.real-session-slow`(纯在叙述"这次测试跑本身没在 2 分钟内跑完")和
`repo.github-remote`(夹带了 GitHub 新建 repo 的一次性 `echo README` 指令)。二者
正是「不要 step-by-step 叙述」本该滤掉却漏掉的。**修 `BuildPrompt`**:在 fact 定义里
加一条排除规则——凡只对"本次 session/运行"成立而非对项目成立的(命令耗时、工具超时、
一次性 setup、关于蒸馏工具自身的观察)一律丢弃,判据是「一个没见过这次 session 的
同事一个月后还需要它吗」。

运维教训:441-turn 真实 session 的 `claude -p` 蒸馏需要**几分钟**,120s 默认超时不够,
要后台跑(这条本身就印证了 real-session 慢——但它是运维知识,不该进 team 知识库,
所以留在这份 progress.md,不留成 fact)。

### 2026-07-21 — L5 蒸馏质量 eval harness 落地(骨架)

`cmd/eval`(非 `_test.go`,`go test`/CI 永不触发,`go build` 编译防腐;`make eval`
手动跑,花真 token)。每个 case = `testdata/corpus/<name>/{transcript.jsonl,
expected.md}`。复用生产管线 `DigestSession → distill.Run(ClaudeRunner)` 出
proposal,再用第二次 `claude -p` 当 LLM-judge 打三轴分(召回/精度/粒度,各 0–5),
输出打分表 + `eval/last-run.json`,与 `eval/baseline.json` 逐 case 比 delta。
`make eval-list` 不花 token 列 case。附一个合成 `example-ci-pg-image`(CI 用错
Postgres 镜像 + 部署顺序坑,10 turns 过 minTurns)只为验证接线;真实语料待 M1。
**这补上了测试方案的最后一层——L0–L5 结构齐了。** 至此 `BuildPrompt` 的每次改动
都有可量化的质量回归信号(M1 的核心悬念第一次有了机器化的度量入口)。

### 2026-07-21 — L1 覆盖率补齐到 ≥85%(每个 internal 包)

按 testing.md L1 目标补错误路径:`ir`(`store_test.go`:`Exists`、`Init`/
`ensureLines` 升级就地、`LoadPlaybooks`/`WritePlaybook` 往返与坏文件隔离、
`SortFacts` 确定性)、`distill`(`prompt_apply_test.go`:`BuildPrompt` 注入分支、
`Run` 的 Now 缺省与错误传播、`Apply` 写失败中止)、`queue`(Enqueue 建目录失败、
List 跳过非 .path)、`materialize`(坏 fact 文件渲染成警告而非中止、import 追加
到无换行结尾文件不粘连)。结果:redact 100 / queue 92 / ledger 89 / ir 89 /
materialize 87 / claudecode 86 / distill 85。`cmd/still` 的 0% 是黑盒子进程测不计数,
逻辑由 L3 实测。

### 2026-07-21 — L2 fuzz 落地 + nightly CI + 一次"假挂起"的排查

四个 fuzz target 落地(容错解析不变量):`FuzzParseProposal`(distill)、
`FuzzParseFact`/`FuzzParsePlaybook`(ir)、`FuzzDigestSession`(claudecode)。
断言:任意输入不 panic;**被接受的输出必须 self-consistent**——fact/playbook
过 `Validate` 且 `Encode` 是不动点(load/write 两侧对"什么算合法"必须一致),
proposal 幸存的 fact id 不能逃出目录、Status==active、body 无残留 secret;
digest 保持 UTF-8、给出 LastActivity、不超预算。全部跑通,**无真实 crasher**。

`.github/workflows/fuzz.yml`:nightly + 手动,四 target 矩阵,`-fuzzminimizetime`
封顶,crasher 上传成 artifact。

**一次假警报的教训**:`FuzzParseProposal` 反复出现"`execs: N (0/sec)` 卡死几十秒"。
逐个 benchmark 被测函数(`parseProposal`/`Validate`/`redact.Text`)全是微秒~毫秒级,
都不是慢路径。SIGQUIT 抓 worker 栈,拿到的"失败输入"直接单跑 **0.01s 通过**——
是红鲱鱼。真因:Go fuzz 引擎对每条新 interesting 输入**内联最小化**,大输入
(10KB body)在默认无界最小化预算下把 `execs` 计数器冻住,**看着像挂,其实在缩样本**,
不是 Stillroom 的 bug。`-fuzzminimizetime` 封顶即恢复正常。redact 用 RE2(线性、
无灾难性回溯),从一开始就排除了正则爆炸的可能。

### 2026-07-20 — L4 融合场景:核心赌注被验证,同时炸出两个 bug

`cmd/still/fusion_test.go`:两个 clone 各自蒸馏 → `git merge`,直接验证
design-v2 §2「一个 fact 一个文件 = git 目录合并就是融合算法」。

**赌注成立**:不相交的知识自动合并,两条 fact 都活下来;两边改同一条 fact 时
照样冲突——这是对的,真正的分歧就该停下来等人裁决——且冲突严格局限在那一个
文件里,其余 fact 照常合并,`still status` 在冲突态下仍可用。

但这条测试炸出两个 bug:

3. **生成物必然冲突**:`materialized.md` 是整份重新渲染的,双方并行蒸馏 →
   **每一次都冲突**,哪怕 fact 本身合得干干净净。事实平面的赌注是对的,坏在旁边
   那个生成物上。解法:`init` 写入 `.team-context/.gitattributes` 声明
   `materialized.md merge=union`——union 是 git **内置**驱动,随仓库提交、每个
   clone 白拿、无需任何 per-clone 配置;双方新增的行都保留,下次
   `still materialize` 按确定性顺序重渲归位。fact 本身**刻意不** union:一条
   fact 上的真实分歧必须停下来问人。
4. **空目录不进 git → 新人 onboarding 崩**:`init` 建的 `facts/`、`playbooks/`
   是空目录,git 不跟踪,队友 clone 下来根本没有这两个目录,**第一次 distill 直接
   崩在裸 ENOENT 上**。修:`init` 写 `.gitkeep`,且 `WriteFact`/`WritePlaybook`
   写前 `MkdirAll` 兜底。

### 2026-07-20 — 测试方案落地(第一批)+ 两个真 bug

测试方案见 `docs/testing.md`:按**不变量**而非按包组织,分 L0–L5 六层,除
「蒸馏质量」外全部可自动化。本批落地 L0/L2/L3:

- `cmd/still/harness_test.go`:CLI 黑盒 harness。每 case 一个隔离世界(临时 git
  repo + 临时 `CLAUDE_CONFIG_DIR` + fake `claude`),**PATH 只含 fake bin 目录**,
  保证「机器上没装 claude」这类分支在开发机上也能真实复现。
- `hook_contract_test.go`:14 种坏输入断言 exit 0 且静默。
- `cli_test.go`:init/distill/status/doctor/materialize 全命令矩阵,含 10 种
  畸形模型输出(散文、截断 JSON、路径穿越 id ……)。
- `privacy_test.go`:10 种 secret 形状,断言蒸馏后**仓库内任何文件**不含原文。
- `internal/ir/invariants_test.go`:确定性与供替单向的随机化属性测试。
- `repo_rules_test.go`:零依赖由机器守;「提交后重跑不产生 git diff」。

新测试当场抓到两个 bug:

1. **hook 契约违规**:`still hook <未知名字>` 走 `return fmt.Errorf` → exit 1,
   违反「任何情况静默 exit 0」。已改为静默 no-op(插件与 CLI 版本错位不是用户的问题)。
2. **`observed_at` 接错了时间源(语义级)**:原本取 `time.Now()`,即**跑蒸馏的时刻**。
   后果是同事今天蒸馏三周前的老 session,产出的 fact 会凭「跑得晚」压掉昨天刚学到的
   新知识——供替规则的排序键接到了工具运行时间上。改为取 session 自身的最后活动
   时间(`SessionMeta.LastActivity`:逐行 `timestamp` 取最大,缺失则回落文件 mtime)。
   连带修 `WriteFact`:RFC3339 是秒精度,带纳秒的时间戳(如 mtime)永远「晚于」自己
   重新解析后的版本,导致每次重写都伪造一条 supersedes——写入前统一截断到秒。

### 2026-07-20 — M1 代码侧完成

- `internal/adapter/claudecode/discover.go`:cwd → Claude Code 存储目录编码(`EncodeProjectDir`),`still distill` 不装插件即可自动发现本 repo 历史 session。**首次体验改变:第一分钟就能蒸馏过去几周的工作。**
- `internal/ledger`:`.team-context/.local/distilled.jsonl` 蒸馏台账(gitignored、append-only、坏行不致损),distill 幂等;`--force` 重蒸。
- `internal/queue`:hook 队列抽成独立包(指针文件、路径哈希幂等、悬空条目自动清理)。
- `internal/distill/similar.go`:rune-bigram Jaccard 近重复检测(中文友好),新 fact 与现有 fact 相似但 id 不同时打 NOTE。
- prompt 增补:现有 playbook(id — title)注入,引导修订而非另铸。
- `still doctor`:六项环境自检。
- `.team-context/.gitignore` 就地升级机制(queue/ + .local/)。

### 2026-07-20 — 仓库迁移与更名

- 项目定名 **Stillroom**(蒸馏房,§16),迁入独立仓库 `~/code/stillroom`。
- CLI 更名 `tg` → `still`;解析器重构到 `internal/adapter/claudecode/`(为 Codex/Cursor 预留)。
- 服务端包袱(server/migrations)留在旧仓库 traces-git,对应 Phase 2 商业侧——本仓库 = "单机 + git 全开源"的那一半(§14 分界线)。
- 补齐 README(典故 + 架构 + 隐私承诺)、Apache-2.0 LICENSE、CI(gofmt/vet/test)、.gitignore。

### 2026-07-19 — M0 骨架(于 traces-git 仓库)

- 五个内部包 + CLI + Claude Code 插件成型,全部单测通过,fake-claude 端到端冒烟通过(含脱敏验证)。
- 关键语义落地:fact 的 `observed_at`/`supersedes`(新观察覆盖旧观察,旧的不可反向覆盖)、materialize 只注入 active、确定性渲染零 git 噪音。

## 下一步(按优先级)

1. **真实 session 蒸馏验证(只有人能做)**:`make still && ./bin/still init && ./bin/still doctor`,然后 `./bin/still distill --dry-run`。真实成色决定 prompt 怎么调(`internal/distill/distill.go` 的 `BuildPrompt`)。这是 M1 的全部悬念。
2. 依据 1 的结果迭代 prompt / fact 粒度 / minTurns 阈值。
3. ~~Codex adapter~~ ✅ 2026-07-22 已落地(见变更日志)。下一个 adapter 候选:Cursor。
4. ~~GitHub Action:PR 上自动评论知识 diff 摘要~~ ✅ 2026-07-22 已落地(`internal/review` + `knowledge-diff.yml`)。
5. M2 发射清单:域名(stillroom.dev / .ai)、GitHub org、商标检索、Show HN、回帖 anthropics/claude-code #38536 / #40981。

## 决策日志(为什么是现在这个样子)

| 日期 | 决策 | 依据 |
| --- | --- | --- |
| 2026-07-19 | 两平面架构:证据不融合,只有蒸馏后的知识融合 | design-v2 §1;对话本身不可 merge |
| 2026-07-19 | 知识平面 = 真实 git repo,一个 fact 一个文件 | merge/review/权限/历史白拿(§2) |
| 2026-07-19 | 蒸馏经用户自己的 `claude -p`,本地运行 | 转录不出机器 = 隐私底线(§4.1) |
| 2026-07-19 | hook 只入队,不自动花 token | 不经同意不调模型;opt-in 自动模式在路线图(§13) |
| 2026-07-19 | 蒸馏输出二次脱敏 | 蒸馏是浓缩器不是消毒器(§4.1) |
| 2026-07-20 | 命名 Stillroom;CLI `still` | §16;Engram/Tacit/Cairn/Baton 等全撞名 |
| 2026-07-20 | 先开源,单机+git 免费 / 中央服务收费 | §14;信任、标准采纳、护城河在语料 |
| 2026-07-20 | 发现走 encoded-cwd 目录但标注 version-fragile,hook 路径优先 | session 上云趋势,at-rest 解析不可长期押注(§11.4) |
| 2026-07-20 | 近重复检测用 bigram Jaccard 而非嵌入 | 零依赖的 PR 级 tripwire;真正的实体消解留给 research(§10) |
| 2026-07-20 | 测试按不变量组织,硬规则即规格 | 六条硬规则是正确性定义,不是风格建议;一条不变量一层可执行证据(testing.md) |
| 2026-07-20 | `observed_at` 取 session 最后活动时间,不取蒸馏时刻 | 供替必须按「知识何时被观察到」排序;按工具运行时间排序会让补蒸历史 session 压掉新知识 |
| 2026-07-20 | `materialized.md` 用 union 合并,fact 本身不用 | 生成物的并行重渲是必然冲突且无信息量;fact 上的分歧则必须停下来问人(§2) |
| 2026-07-21 | fuzz 只进 nightly 且 `-fuzzminimizetime` 封顶,不进 push/PR 门禁 | fuzz 是时间盒探测不是门禁;引擎内联最小化在大输入上会假性卡住,封顶保证 nightly 运行有界可预测 |
| 2026-07-22 | `Digest`/`Meta` 下沉到 `internal/session`,adapter 只做「format→digest」 | 多工具支持的正确边界:管线认一个 tool-agnostic 类型,加工具=加 adapter,零下游改动 |
| 2026-07-22 | Codex 的 `developer` 消息丢弃,只留 user/assistant | 注入的环境/审批/base-instructions 是框架不是知识,且会吃满 digest 预算(同 claudecode 丢 thinking) |
