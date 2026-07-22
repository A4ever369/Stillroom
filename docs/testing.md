# Stillroom 测试方案

> 原则:**硬规则即规格**。CLAUDE.md 里那六条不变量不是风格建议,是这个项目的
> 正确性定义。测试按不变量组织,而不是按包组织——一个不变量对应一层可执行的证据。

## 为什么可以全自动

没有 GUI,没有网络依赖,没有并发调度。系统的全部表面是:

| 表面 | 形态 | 可测性 |
| --- | --- | --- |
| 解析 transcript / frontmatter | 纯函数 `[]byte → 结构体` | 单测 + fuzz |
| 知识渲染 | 纯函数 `结构体 → []byte` | golden + 属性 |
| 文件系统效果 | 临时目录内可完全隔离 | 黑盒 CLI |
| 模型调用 | `Runner` 接口,可注入 | fake runner,零 token |
| Claude Code 存储 | `CLAUDE_CONFIG_DIR` 可重定向 | 合成语料 |

唯一不能全自动的是**蒸馏质量**(模型输出好不好),那一层单独隔离到 L5,
用金标准语料把它变成半自动,且不进 CI。

## 分层

### L0 静态门禁(秒级,每次 push)

- `gofmt -l .` 为空
- `go vet ./...`
- `go build` 跨平台:linux/amd64、darwin/arm64、windows/amd64(交叉编译即可,不需运行)
- **零依赖断言**:测试直接读 `go.mod`,断言没有 `require` 块。这条硬规则目前
  只靠人自觉,应该由机器守。

### L1 单元测试(已有,补齐)

**目标已达成**:每个 internal 包 ≥ 85%——redact 100%,queue 92%,ledger 89%,
ir 89%,materialize 87%,claudecode 86%,distill 85%。补齐重点在 `ir`(store 的
错误路径:`Exists`/`LoadPlaybooks`/`WritePlaybook`/`ensureLines` 升级、`SortFacts`)
与 `distill`(`BuildPrompt` 注入分支、`Run` 错误传播、`Apply` 写失败),另加 queue
的错误分支与 materialize 的坏文件警告/import 追加分支。

`cmd/still` 的 `-cover` 显示 0%,是因为黑盒测试把编译后的 binary 当子进程跑,
`go test -cover` 量不到——其逻辑由 L3 `harness_test.go` 等实测覆盖,只是不计数。

### L2 不变量测试(本方案的核心)

这一层是新增的,一条不变量一个测试文件,失败信息直接指向被违反的硬规则。

| 不变量 | 测试形态 | 断言 |
| --- | --- | --- |
| **确定性** | 属性测试 | 随机 fact 集合 → `Encode()` 两次字节相同;打乱写入顺序不影响 `materialized.md`;连跑两次 materialize 后 `git diff` 为空 |
| **供替单向** | 属性测试 | 随机 `observed_at` 序列以任意顺序写入,终态恒等于「最新观察」;旧观察永不覆盖新的 |
| **隐私** | 语料驱动 | secret 形状语料库(AWS key / JWT / PEM / `password = "..."` / bearer token / 中文上下文里的密钥)全部被 `redact` 清除;且经 fake runner 注入 secret 后,断言**磁盘上任何文件**不含原文 |
| **容错解析** | fuzz | `FuzzDigestSession`、`FuzzParseFact`、`FuzzParseProposal`:任意输入不 panic;半坏的 jsonl 仍产出前面的好行(不整文件失败) |
| **hook 契约** | 表驱动黑盒 | 十几种坏输入(空 stdin / 非 JSON / 缺字段 / 巨大 payload / 不存在的 cwd / 未 init 的 repo / 未知 hook 名)一律 **exit 0 且 stdout+stderr 为空** |

> 当前已知违规:`still hook bogus` 走 `return fmt.Errorf(...)` → exit 1。
> hook 契约测试落地即会红,这就是这层的价值。

### L3 CLI 黑盒测试(填 428 行的 0%)

Go 测试内编译一次 binary,每个 case 起一个隔离世界:

```
tmp/
  repo/            git init 过的假项目(cwd)
  claude-home/     CLAUDE_CONFIG_DIR,放合成 transcript
  bin/claude       fake claude,按 case 吐不同 JSON
```

用表驱动跑命令矩阵,断言 exit code + stdout 快照 + 文件系统终态:

- `init`:全新 repo / 重复跑(幂等)/ 已有 CLAUDE.md(追加不覆盖)/ 非 git 目录(报错退 1)
- `distill`:无 session / 太短的 session(minTurns)/ 正常 / `--dry-run` 不落盘 /
  `--force` 重蒸 / `--transcript` 指定 / fake runner 返回垃圾 JSON / fake runner 超时
- `status`:空库 / 有坏文件(报 BAD 但不崩)
- `doctor`:六项各自失败时的输出与退出码
- `materialize`:空库 / 只有 archived fact
- `hook`:见 L2

fake claude 用一个可参数化的脚本(读环境变量决定吐什么),让「模型返回畸形输出」
这类分支也能测——这是目前完全没覆盖的一大片。

### L4 端到端场景 —— **矩阵已落地**

`scripts/smoke.sh` 已从单条 happy path 扩成**场景矩阵**(纯 bash + fake claude,
零 token)。每个场景一个隔离世界(临时 repo + `CLAUDE_CONFIG_DIR`/`CODEX_HOME` +
fake claude),失败局部化、场景间不串状态。跑的是**编译出的真二进制**,测的是
用户实际体验的那条链路(shell 集成、插件 hook、真实文件系统):

1. **冷启动全流程** ✅:init → doctor → 自动发现 → distill(含脱敏断言)→ materialize
2. **hook 入队路径** ✅:`still hook session-end` 读 `{transcript_path,cwd}` 入队 →
   队列文件出现 → `status` 显示 pending 1 → distill 消费 → 队列清空、pending 0。
   (转录刻意放在发现目录**之外**,确保唯一入口是队列。)
3. **幂等与 force** ✅:重跑报「nothing to distill」;`--force` 重新蒸馏
4. **Codex 发现**(新增)✅:CODEX_HOME 放一个 cwd 匹配本 repo 的 rollout,空
   CLAUDE_CONFIG_DIR → distill 端到端发现并蒸馏它,验证多工具接线走通真二进制。
5. **融合**(`cmd/still/fusion_test.go`,Go 黑盒):两个 clone 各自蒸馏 →
   `git merge`。验证 design-v2 §2「一个 fact 一个文件」的核心赌注。**结论:赌注成立**
   ——不相交的知识 git 自动合并;真正的分歧(两边改同一 fact)照样停下来等人裁决,
   且冲突严格局限在那一个文件里,其余知识照常合并。
   > 这条测试同时逼出了生成物 `materialized.md` 的必然冲突(见变更日志),
   > 以及空目录不进 git 导致的新人 onboarding 崩溃。
6. **升级路径**(新增)✅:手工造老版本 `.team-context/` 布局(有 fact、`.gitignore`
   缺 `.local/`、无 `.gitattributes`、无 `.gitkeep`)→ `still init` 就地升级 →
   断言补齐 union-merge 属性 / `.local/` / 两个 `.gitkeep`,且**原有 fact 不丢**。
7. **review diff**(新增)✅:`still review --base/--head` 渲染语义知识 diff,断言
   锚点 + 新 fact 出现、未变 fact 不出现。

### L5 蒸馏质量评估(半自动,不进 CI)——**骨架已落地**

唯一需要真 token 的一层,单独 `make eval`(`cmd/eval`,不是 `_test.go`,所以
`go test ./...` / CI 永不触发它;但 `go build ./...` 会编译它防腐)。机制:

- 每个 case 是 `testdata/corpus/<name>/` 下的一对文件:`transcript.jsonl`
  (脱敏后的真实 session)+ `expected.md`(主题级标注「本该学到什么」,含
  「不该编造什么」的负样本)。格式见 `testdata/corpus/README.md`。
- 复用**生产管线**:`DigestSession → distill.Run(ClaudeRunner)` 出 proposal,
  再用第二次 `claude -p` 当 LLM-judge,对 proposal 打三轴分(0–5):召回(该学的
  学到没)、精度(有没有编造)、粒度(fact 太糙还是太碎)。
- 输出打分表,写 `eval/last-run.json`,与 `eval/baseline.json` 逐 case 比 delta。
  冻结基线:`cp eval/last-run.json eval/baseline.json` 后提交。
- `make eval-list` 不花 token,只列将评估的 case。

这层不阻塞 CI,但**改 `BuildPrompt` 前后必须跑一次**。当前 corpus 只有一个合成
`example-*` case(仅验证 harness 接线);等 M1 真实语料就位后替换为真实 session。

## CI 编排

| 任务 | 触发 | 时长目标 |
| --- | --- | --- |
| L0 + L1 + L2 + L3 | 每次 push / PR | < 60s |
| L4 场景矩阵 | 每次 push / PR | < 90s |
| fuzz(各 target 5 分钟) | nightly(`.github/workflows/fuzz.yml`) | — |
| L5 eval | 手动 / prompt 改动的 PR 打标签触发 | — |

> **fuzz 运维注意**:Go 的 fuzz 引擎对每条"新 interesting"输入会**内联最小化**,
> 大输入(如 10KB body)在默认(无界)最小化预算下会让 `execs` 计数器停在
> `0/sec` 几十秒——**看着像挂了,其实是引擎在缩样本**,不是被测代码的 bug。
> nightly 用 `-fuzzminimizetime 30s` 封顶,本地复现同理。四个 target 均已跑通、
> 无真实 crasher。

## 落地顺序

1. L2 hook 契约 + 隐私(直接抓已知违规)
2. L3 CLI 黑盒骨架(收益最大:0% → 大头)
3. L2 确定性 + 供替属性测试
4. L4 场景 4(融合)——验证架构赌注 ✅
5. fuzz targets + nightly ✅
6. L5 eval 骨架 ✅(harness 就绪,待 M1 真实语料替换合成 example)
