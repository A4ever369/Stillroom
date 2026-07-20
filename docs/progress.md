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
