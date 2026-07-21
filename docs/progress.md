# Stillroom 进度文档

> 维护约定:每完成一个里程碑或做出一个影响方向的决策,在这里追加一条(带日期)。
> 本文档是项目的"进度事实源";设计原理见 `docs/design-v2.md`。

## 状态总览

| 里程碑 | 内容 | 状态 |
| --- | --- | --- |
| M0 骨架 | ir / redact / adapter / distill / materialize / CLI / 插件,全部带单测 | ✅ 2026-07-19 |
| M1 自食 | session 自动发现、台账、近重复防护、doctor;**真实 session 蒸馏质量验证** | 🚧 代码侧就绪,待真实数据验证 |
| M2 开源发布 | repo 公开、发射动作(§14)、第一批外部用户 | ⬜ |
| M3 融合验证 | 双人 merge 三条路径、任务级评估 | ⬜ |
| M4 服务端(Phase 2) | 证据库、回放、检索、MCP 面;商业化启动 | ⬜ |

## 变更日志

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
3. Codex adapter(`~/.codex/sessions/**`,实现 `internal/adapter/codex`,复用 digest→distill 管线)。
4. GitHub Action:PR 上自动评论知识 diff 摘要(review 寄生策略的最后一块,§13)。
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
