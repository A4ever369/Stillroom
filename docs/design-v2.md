# Traces Git —— 设计 v2:两平面架构与 git 底座

> 取代关系:本文档是 `session-ir-research-brief.md`(研究简报)和 `architecture.md`(v1 蓝图)之后的第三份文档,**收敛为可实施设计**。与前两份的差异见 §9。
>
> 一句话:证据不可融合、知识可融合——把系统沿这条线切成两个平面,知识平面直接用真实 git repo 承载,让可融合单元与 git 的可融合单元重合,merge/review/权限/历史全部白拿。

---

## 0. 问题重述

团队协作时,每个人和自己的 coding agent(Claude Code / Codex / Cursor…)的对话、经验、历史,要能像代码库一样被维护和传递:我能把我的经验"push"出去,同事能"pull"下来**融合**进自己的上下文,带着融合后的记忆继续和 AI 干活。不是 session 覆盖,是融合。

核心矛盾:**对话本身不可 merge**(两段线性对话没有有意义的合并结果),但**从对话里学到的东西可以 merge**。整个设计围绕这一条展开。

## 1. 两平面架构

```
┌─ 证据平面(Evidence Plane)────────────────────────────┐
│  原始 session 转录(jsonl 等),append-only,内容寻址,   │
│  永不 merge,只被引用。                                  │
│  价值 = 回放 / 溯源 / 审计 / fork 的种子 / 深挖细节        │
└────────────────────┬─────────────────────────────────┘
                     │ distill(蒸馏:本地跑,产出 diff,人审)
                     ▼
┌─ 知识平面(Knowledge Plane)───────────────────────────┐
│  facts(事实)+ playbooks(配方)+ skills,               │
│  小文本文件,存放于一个真实 git repo("团队知识库")。      │
│  可 merge、可 review、可版本化、可 blame。                │
│  这是团队真正协作维护的对象。                              │
└────────────────────┬─────────────────────────────────┘
                     │ materialize(物化:渲染成工具可注入形态)
                     ▼
        CLAUDE.md / AGENTS.md / memory/ / skills/ / MCP resources
                     │
                     ▼
          任何工具的新 session 天然带上团队融合后的记忆
```

设计不变式:

1. **融合只发生在知识平面**。证据平面只累积、只引用,从不参与 merge。
2. **知识平面的每一条内容都必须能指回证据平面**(provenance),但反向不要求——允许存在未蒸馏的证据。
3. **共享边界 = 知识平面**。默认只有蒸馏物进入团队库;raw transcript 留在本机或私有证据库,共享是显式动作。

## 2. 关键决策:知识平面用真实 git repo 承载

**可融合单元 = git 的可融合单元:一个 fact 一个文件,一个 playbook 主题一个文件。**

这个映射买到了什么:

| 需求 | git 原生给的 |
| --- | --- |
| 融合:两人各自学到不同事实 | 目录级 merge = 集合并集,零冲突 |
| 冲突:同一事实两个值 | 同文件冲突 → 恰好就是需要人裁决的场景 |
| 共享前人工确认 / 脱敏预览 | PR review 流程,现成 |
| "这条经验谁在哪次 session 学到的" | `git blame` + frontmatter provenance |
| 回滚 / 历史 / 分支 / 团队权限 | git 原生 |

**推论**:v1 蓝图里的自建 merge 引擎、trace 级 grant 服务、ingest API 在 MVP 阶段全部不需要。它们不是被否定,而是被推迟到证据平面和企业形态需要时(§8 Phase 2+)。

诚实的取舍:

- 权限粒度 = repo 粒度,没有 trace 级 grant。初期一个团队一个知识库,够用;细粒度是 Phase 2 上服务端的理由。
- 语义搜索初期没有。物化后的全文本身就在 agent 的上下文/工作区里,agent 自己 grep 已覆盖大部分场景;pgvector 检索属于证据平面服务(Phase 2)。
- LLM 参与的 playbook 综合是**非确定性**操作。不假装它是 git 式确定性 merge:综合结果作为一次新 commit 带完整 provenance 存下来,不承诺可重现(见 §5)。

## 3. 知识库 repo 布局

```
team-context/                      # 团队知识库(真实 git repo)
├── facts/
│   ├── deploy.acme.db-endpoint.md
│   ├── build.monorepo.pnpm-quirk.md
│   └── ...                        # 一个 fact 一个文件,文件名 = 语义键
├── playbooks/
│   ├── customer-onboarding-deploy.md
│   └── ...                        # 一个主题一个文件
├── skills/
│   └── ...                        # 晋升后的组织 skill(标准 SKILL.md 结构)
├── evidence-index/
│   └── index.jsonl                # 证据指针(trace ref → 存放位置),不含转录本体
└── .tg/
    └── config.yaml                # 物化目标、脱敏规则、作用域映射
```

### 3.1 fact 文件

```markdown
---
id: deploy.acme.db-endpoint            # 语义键 = 事实身份,同时是文件名
scope: repo:acme-infra                 # 适用范围(repo / 环境 / 全局)
observed_at: 2026-07-18T09:30:00+09:00 # 观察时间 —— 时效性语义的载体
source: trace://allen/a3f9c2/turns/41-58  # 指回证据平面
confidence: high                       # high | medium | low
status: active                         # active | superseded | disputed
supersedes: deploy.acme.db-endpoint@2026-05-02   # 可选:覆盖的旧观察
---
Acme 生产库的入口是 pgbouncer 而非直连,端口 6432,
直连 5432 会被安全组拦。
```

要点:

- **`observed_at` 与 `supersedes` 是融合语义的一部分,不是元数据装饰**。事实会过期;没有时效标注的并集只会累积陈旧垃圾。
- 物化器只注入 `status: active` 的 fact;`superseded` 保留在历史里供追溯;`disputed` 进 review 队列。
- 正文用自然语言,一条 fact 说一件事,长度以"能独立注入且自明"为限。

### 3.2 playbook 文件

playbook = 某类任务的可复用配方(昆卡剧本里的"客户上线部署"就是一个 playbook)。结构:适用前提 → 步骤 → 已知坑(链接相关 facts)→ 证据链接(源 trace)。playbook 是蒸馏的高阶产物,一般由一次成功 session 首建,后续 session 修订。

### 3.3 fact 身份(去重)的务实策略

"两条 memory 是不是同一事实"是真正的研究难题(参见研究简报开放问题 2),MVP 不求完美解:

1. 蒸馏 LLM 负责提议语义键(`域.对象.属性` 风格 slug),提议前先读现有 facts/ 目录做键对齐;
2. 合并时用嵌入相似度跑一遍近重复检测,疑似重复标注进 PR 让人裁决;
3. 键冲突且值不同 → git 冲突 → 人裁决,裁决结果本身成为一次 commit(不会重复裁决,因为 git 有共同祖先)。

## 4. 三个要写的组件

底座交给 git 后,自研面收敛为三件东西。

### 4.1 Distiller(蒸馏器)—— 核心壁垒,唯一的 AI 组件

- 输入:本地 session 存档(Claude Code `~/.claude/projects/<encoded-cwd>/*.jsonl` + memory 目录;Codex `~/.codex/sessions/**`;Cursor store)。
- 输出:**对知识库 repo 的一个 diff**——新增/更新哪些 fact 文件、修订哪个 playbook,附 provenance 指针。
- 约束:**必须本地运行**(转录不出机器);产出经脱敏(`redact.*` 可复用)后以 PR/commit 形式提交。
- 人在环:PR review 就是脱敏预览 + 质量把关。**警惕"蒸馏天然脱敏"的直觉——它可能是反的**:raw transcript 泄密是偶然的,蒸馏后的 fact 是刻意浓缩的("prod 凭证在 vault 的 key X 下"恰恰是蒸馏最想保留的),所以蒸馏层必须过 redact + 人审,一个不能少。
- 解析侧复用:Multica 已把 15 个 runtime 归一化成统一 turn 形状(`agent.Message`),export 侧 ~80% 现成;区别是要新写 at-rest 静态文件解析(容错 + 版本探测,不硬解)。

### 4.2 Materializer(物化器)—— 几乎工具无关

行业正在收敛到可注入上下文标准:CLAUDE.md / AGENTS.md、memory 目录、skills、MCP resources。物化器把知识库渲染成这套形态:

- 生成一段带标记的 CLAUDE.md/AGENTS.md 区块(按 `scope` 过滤当前 repo 相关的 facts);
- 落 memory 文件与 skills 目录;
- (Phase 2)以 MCP resource 形式动态供给。

**成本不对称是本设计的重要红利:import 侧一套通吃,逐工具的苦活只在 export 解析侧。**适配器矩阵砍半。

### 4.3 Evidence store(证据库)

- 转录大(实测单会话 10–40MB),不进 git;放对象存储(自托管 MinIO 即可),内容寻址,`source:` 指针指进去。
- MVP 可以更薄:先只在 `evidence-index/index.jsonl` 里登记指针(机器 + session id + 内容 hash),转录留在原机;回放/检索需求出现时再集中上传。
- Phase 2 在其上建:回放 UI(复用 `buildTimeline()`)、pgvector 语义搜索、MCP `search_traces / read_trace / fork_trace`。

## 5. 融合语义(按层,最终版)

| 层 | 融合方式 | 冲突处理 | 确定性 |
| --- | --- | --- | --- |
| facts | 文件级集合并集;同源同键按 `observed_at` 覆盖(supersede) | 异源同键异值 → git 冲突 → 人裁决 | 确定性(git) |
| playbooks | LLM 综合修订,产出为新 commit | 语义矛盾标注给人 | **非确定性,靠 commit 固化** |
| skills | 按名去重、版本递增 | 版本分叉走 PR | 确定性 |
| 转录(证据) | **永不 merge**;只作为 provenance 被引用 | 无 | — |
| 代码/产物 | 真 git merge(在代码 repo 里) | git 冲突流程 | 确定性 |

与 git 类比的边界要说清楚:git merge 的魔力在于确定性、可重现、笨,语义负担全推给人。facts 层保住了这个性质;playbooks 层做不到(LLM 综合),所以**不把它伪装成 merge,而是当作一次 agentic 修订操作**,结果 commit 下来,provenance 记全(输入了哪些源 trace / 哪些旧版本)。

## 6. `tg` CLI 命令面

```bash
tg init                  # 关联团队知识库 repo + 本地工具存档路径
tg distill [--session S] # 本地蒸馏:session → 知识库 diff(含脱敏),开 PR 或本地 commit
tg push                  # = git push(语义糖:推知识库 + 上传新登记的证据指针)
tg pull                  # = git pull + 自动 materialize
tg materialize [--repo R]# 知识库 → CLAUDE.md 区块 / memory / skills(按 scope 过滤)
tg status                # 本地有哪些未蒸馏 session、知识库落后多少
tg evidence push <ref>   # 显式上传某段转录到证据库(默认不传)
```

工作流(昆卡剧本,MVP 版):

1. Allen 部署完客户环境 → `tg distill` 本地生成 PR:1 个 playbook + 若干 facts,全部带 provenance;
2. Allen 扫一眼脱敏预览,merge PR;
3. 昆卡 `tg pull`(或她的工具通过钩子自动做)→ 物化器把 playbook 和相关 facts 注入她的 CLAUDE.md/memory;
4. 她的 Claude Code 照配方干完;卡住时(Phase 2)`read_trace` 翻 Allen 的原始证据。
5. **Allen 全程没被打扰;且 MVP 闭环中没有任何一个自建服务是必需的——一个 CLI + 一个 git repo。**

## 7. 承重假设与验证顺序

**第一承重假设是"再物化有效",不是"融合可行"**:蒸馏后的上下文注入新 session,真的能让另一个人(的 AI)接着干。融合的价值依赖于它——若注入无效,merge 得再漂亮也没意义。

验证顺序(对研究简报 §9 的顺序调整):

1. **再物化 PoC(最先)**:拿一次真实 session → `tg distill` → 物化进新 session → 另一人执行同类任务。度量:相对冷启动的 token 消耗、重复探索次数(重新 Read 已知文件)、任务完成质量。
2. **融合 PoC**:两人对同一环境各自干活 → 各自蒸馏 → git merge 知识库 → 验证并集/覆盖/冲突三种路径,且验收标准是任务级的(融合后的记忆是否让第三人干得更好),不是"打印并集效果"。
3. 之后再谈证据库集中化、回放、语义搜索、MCP 面、多工具矩阵。

## 8. 路线图

- **Phase 1(验证核心假设,~1–2 周量级)**:`tg` CLI(init/distill/materialize/pull/status)+ Claude Code 单工具 export + 团队知识库 repo + PR 工作流。自己团队当第一个用户,跑通昆卡闭环。**交付形态优先做成 Claude Code 插件(hook + skill),而非独立 CLI**——见 §13。
- **Phase 2(证据平面服务化)**:证据库集中上传、回放 UI、pgvector 语义搜索、MCP server(`search_traces / read_trace / fork_trace`)、Codex/Cursor export。此时 v1 蓝图的 ingest API / tenant 隔离 / grant 模型按原设计启用。
- **Phase 3(企业形态)**:SaaS 多租户加固(RLS、配额、计费)、trace 级细粒度 grant、组织 skill 晋升管线(fact/playbook → skill,人在环)、license 门。

## 9. 与前两份文档的差异对照

| 维度 | v1 蓝图 / 研究简报 | 本设计(v2) | 理由 |
| --- | --- | --- | --- |
| 首个底座 | 自建中央服务(ingest + PG + grant) | 真实 git repo | merge/review/权限/历史白拿;先验证假设再建服务 |
| IR 分层 | SessionIR 单对象五层 | 拆成两平面;turns 层归证据平面 | "IR 可 merge"只对蒸馏层成立,合并到一个对象里会诱导对 turns 做 merge |
| 验证顺序 | 融合 PoC 最先 | 再物化 PoC 最先 | 融合价值依赖再物化有效;承重假设先验证 |
| fact 原语 | 事实集合,无时效 | + observed_at / supersedes / status / scope | 事实会过期;无时效的并集累积陈旧垃圾 |
| merge 确定性 | 未区分 | facts 确定性 / playbooks 显式非确定性 | 保住 git 信任模型的同时诚实对待 LLM 综合 |
| 蒸馏与隐私 | 蒸馏视作天然脱敏 | 蒸馏视作**浓缩器**,必须 redact + 人审 | 蒸馏保留的恰是高价值敏感信息 |
| 适配器成本 | export/import 双向逐工具 | import 侧近乎工具无关(CLAUDE.md/skills 标准) | 行业收敛红利,矩阵砍半 |
| lineage | 单父(ForkedFromTraceID) | git 多父 commit 原生支持 | merge 必然多父;避免日后改 schema |

v1 蓝图的护城河积木(15-runtime 归一化、redact、buildTimeline、grant 模型、pgvector、自托管通道)全部保留,只是启用时点后移到 Phase 2/3。

## 10. 开放问题(留给 research,承接研究简报 §7)

保留研究简报的 10 个开放问题,叠加三个文献锚点,避免从零摸索:

1. fact 身份与去重 ≈ entity resolution + truth discovery / knowledge fusion(Google Knowledge Vault 一系),以及 agent memory 系统的去重实践(MemGPT/Letta、Zep Graphiti、Mem0);
2. facts 层收敛语义 ≈ CRDT 文献(grow-only set + 墓碑 + LWW);本设计选择 git 三方 merge 路线,但若 Phase 2 走服务端实时融合,CRDT 是备选;
3. 再物化保真度评估:任务延续基准(同 repo 任务对,冷启动 vs 注入蒸馏上下文),指标 = 完成 token 数、冗余工具调用、正确率、人评"像不像接着原作者干"。

---

## 11. 战略定位:总的 infra,不是 converter

> 定位一句话:**跨工具的团队人机协作知识 system-of-record**。converter 是楔子和获客入口,是 feature 不是 identity。

### 11.1 为什么中立层位置成立

大厂互不兼容是结构性的:Anthropic 不会写 Codex 的 session 解析器,OpenAI 不会替 Claude Code 做记忆导入。厂商互斥的地方就是中立层的生存空间——先例:Terraform 之于各家云、Plaid 之于银行、Segment 之于分析工具、OpenRouter 之于模型 API。且多工具混用是团队常态、个人工具切换周期以月计:**工具 churn 越快,"知识跟人和团队走、不跟工具走"的价值越大**。留存故事恰好建立在工具生态的不稳定上。

### 11.2 三层价值梯度(资源与叙事都按此分配)

1. **Adapters(捕获/转换层)**:苦活不是难活,两周可复制,且标准化会持续摊平其价值(AGENTS.md 已商品化注入侧)。→ **策略:全部开源**,主动当事实标准,让社区维护格式漂移(最烦的部分),商品化竞争对手的捕获层(commoditize the complement,Terraform providers 打法)。
2. **知识平面(产品核心)**:fact 模型、时效语义、融合、provenance、PR review 工作流。自研壁垒所在,商业化载体。
3. **知识语料(护城河)**:session 越多 → facts 越准 → 新 session 越好用,复利增长;且这份资产**跨越任何一次工具更换存活**。system-of-record 位置由它决定。

### 11.3 市场边界(诚实版)

"大厂互不支持"保护的是**多工具团队**这个细分。若 Anthropic 给纯 Claude Code 团队做了够用的原生团队记忆,单工具市场进不去。我们的地盘:混用团队 + 自托管/数据不出企业的刚需客户 + 想要 git 化可 review 记忆的团队。足够大,但要认清边界;竞争格局详见 §12。

### 11.4 捕获耐久性(设计约束)

Session 正在上云(Claude Code on the web、Cursor cloud agents),云上 session 本地没有 jsonl,at-rest 解析会逐渐够不着。**捕获策略不押宝文件解析**:hook/SDK 级捕获(如 Claude Code SessionEnd hook 提供 `transcript_path`)比 at-rest 解析更耐久。Adapter 接口把两种来源抽象为同一输入:`capture source ∈ {at-rest file, hook/SDK stream}`。

## 12. 竞争格局(2026-07 调研快照)

### 12.1 需求信号

- anthropics/claude-code [#38536](https://github.com/anthropics/claude-code/issues/38536):工程经理提出的结构化"共享团队记忆"需求(记忆池、记忆提升、交接上下文转移、事件响应连续性),与本设计场景逐条对应;[#40981](https://github.com/anthropics/claude-code/issues/40981) 要求跨成员共享 session。均 open、无官方回应。
- 社区自救已出现:[claude-session-memory](https://github.com/teamspwk/claude-session-memory)(自动捕获→知识卡→git 共享,思路同构,玩具阶段)、session-share skill 等。
- Issue 中列举的现有 workaround 全部失败于同一点:**要求用户新增动作**(手动重构上下文/导出贴工单/口头交接/静态 CLAUDE.md)。→ §13 的设计原则由此而来。

### 12.2 玩家对照

| 玩家 | 做了什么 | 缺什么 |
| --- | --- | --- |
| Claude Code 原生 | session 严格本机、按目录;团队层面只有静态 CLAUDE.md | 无跨用户共享、无团队记忆、无蒸馏 |
| Cursor 原生 | Memories(个人级)、团队可共享的只有手写 rules | Memories 不跨团队,无 session 蒸馏 |
| GitHub Copilot Spaces | 团队上下文空间 | 内容靠人工策展,只喂 Copilot,单工具孤岛 |
| Mem0 / Zep / Letta | 记忆 API 基础设施,有 org 级 scope | 给开发者造 agent 用的 API,不是团队协作产品;其 2026 报告的开放问题(记忆过时、隐私同意、演进 vs 替换)反向论证本设计 fact schema 的必要性 |
| **SpecStory(最接近)** | 本地优先捕获 7 种工具 session、云同步、Lore 把历史加工成 skills、1.2k star | 捕获/搜索优先;无 fact 模型、无融合语义、无 provenance 链;**团队共享标注"coming soon"** |

### 12.3 窗口与风险判断

- 先发窗口以**季度**计(SpecStory 团队功能在路线图上)。
- 最大风险 = 平台原生化(社区期待强到出现过虚构的"Claude Code team memory 泄漏"文章)。结构性防御 = 平台厂商不会做的三件事:跨工具中立、自托管数据不出企业、git 原生。
- 方向性利好:AGENTS.md 已成跨工具事实标准,Materializer"import 一套通吃"的赌注被行业收敛兑现。

## 13. 零摩擦用户流程(结合现有场景)

**设计原则只有一条:零新增习惯——每个环节寄生在团队已有的动作里。**

| 环节 | 寄生宿主 | 机制 |
| --- | --- | --- |
| 捕获 | 装一次插件 | SessionEnd hook 后台自动蒸馏,无 `tg distill` 手动命令 |
| Review/脱敏 | 本来就要 review 的 PR | session↔PR 原生关联(`--from-pr`);知识 diff 作为伴随 commit / bot 评论进**同一个 PR** |
| 存储 | 现有代码 repo | MVP 不建独立知识库 repo,放 `.team-context/` 目录,权限/clone/CI 全部搭现成的车 |
| Pull + 物化 | `git pull` 代码 | CLAUDE.md 一行 `@.team-context/materialized.md` import;同事拉代码即拉知识,起 session 自动带上——这一步**彻底消失** |
| Onboarding | clone repo | 新人第一天起 Claude Code,团队全部 facts/playbooks 已在上下文里。零动作,比昆卡剧本更普适的 demo 场景 |

用户可见面收敛为两个:**装一次插件 + 在 PR 里多看一段知识 diff**。其余全部隐形。`tg` CLI 保留给高级用户与 debug。跨 repo 的组织级知识库、MCP 检索面等到 Phase 2 再引入。

## 14. 开源与商业化策略

**先开源,商业化跟着 Phase 2 的服务端落地。**三个决定性理由:

1. **隐私信任**:输入是全公司 AI session 转录,闭源 SaaS 起步过不了企业信任关;"开源 + 本地优先 + git 原生"把服务器整个移出数据路径,MVP 本来也不需要服务器。
2. **标准采纳**:§11 的目标是当事实标准,标准必须开放;赛道上直接竞对(SpecStory,Apache-2.0)与社区方案全部开源,闭源连被评估资格都难拿。
3. **护城河不在代码**:知识语料在客户自己 repo 里复利,可收费价值在中央服务层。开源送掉的是本来守不住的部分。

**分界线一句话:单机 + git 能跑的,永远开源免费;需要中央服务的,收费。**

| | 开源(Apache/MIT) | 商业(SaaS 订阅 / 自托管 license) |
| --- | --- | --- |
| 内容 | spec、adapters、tg CLI、插件、distiller、materializer | 集中证据库、回放 UI、语义搜索、MCP 检索面、跨 repo 组织知识库、trace 级权限、SSO/审计 |
| 对应阶段 | Phase 1 全部 | Phase 2/3 全部 |

License 分层:spec 与 adapters 用宽松协议最大化采纳;服务端组件将来可考虑 AGPL/BSL 防云厂商白嫖(Phase 2 再决策,现在不消耗精力)。

**发射动作**(开源 ≠ 扔上 GitHub;同构想法 0 star 的先例就在眼前):Show HN;直接回帖 anthropics/claude-code #38536 与 #40981——第一批目标用户已经在评论区实名画像;配合 AGENTS.md 生态位写一篇"你的团队记忆不该锁死在某个工具里"的定位文。

## 15. 开发计划

- **M0 骨架(已完成)**:`internal/ir`(fact/playbook 模型、frontmatter 编解码、supersession 语义、store)、`internal/redact`(密钥脱敏)、`internal/parser`(digest:转录→蒸馏输入,含元数据提取与 head/tail 截断)、`internal/distill`(prompt 构建、经 `claude -p` 的本地蒸馏、二次脱敏、proposal 校验)、`internal/materialize`(确定性渲染 + CLAUDE.md import)、`cmd/tg`(init/distill/materialize/status/hook)、Claude Code 插件(SessionEnd 入队)。全部带单测,端到端冒烟通过。
- **M1 自食(本周)**:自己团队真实 session 上跑蒸馏,迭代 prompt 质量(这是核心壁垒所在);验证再物化假设——蒸馏后的上下文注入新 session,另一人能否接着干(§7 的第一承重假设);按结果调 fact 粒度与 playbook 结构。
- **M2 开源发布**:拆出独立开源 repo(或本 repo 转公开),LICENSE、README、安装文档、demo GIF;执行 §14 发射动作;收第一批多工具团队反馈。
- **M3 融合验证**:两人对同一环境各自蒸馏 → git merge 知识库,验证并集/覆盖/冲突三条路径;嵌入相似度的近重复检测 bot(PR 上标注疑似同义 fact);任务级评估(冷启动 vs 注入,token/冗余探索/正确率)。
- **M4 Phase 2 服务端**:按 §8,集中证据库、回放、检索、MCP 面;商业化随之启动。

蒸馏自动化的演进路径:MVP 是"SessionEnd 入队 + 手动 `tg distill`"(不经同意不花 token,不做后台 LLM 调用);验证信任后加 opt-in 的自动蒸馏模式(后台 `claude -p --no-session-persistence`,防 hook 递归已内建)。

## 16. 命名:Stillroom

**产品名 Stillroom(蒸馏房)**;Traces Git 降级为内部代号/repo 名沿用。

来历与咬合点:still = 蒸馏器(distill 的词根);stillroom 是 16 世纪起英国庄园里蒸馏药酒、提炼草药的专门房间,其主人世代维护一本 **still room book**——配方、药方、经验条目,由上一代传给下一代、每代人补充自己验证过的新条目。它是历史上真实存在的跨代团队知识库。一个词同时命中产品的两个核心:**distill(蒸馏 session)+ 世代相传的共享手册(.team-context)**。

命名调研结论(2026-07):Engram(已融 $98M)、Tacit、Cairn、Baton、Slipstream、Kindling、Stigmerge 等候选在 AI/dev 工具空间全部被占;Stillroom 在该空间基本干净(仅存在香薰/日记类远距离小品牌)。

待办:注册域名(候选 stillroom.dev / stillroom.ai / getstillroom.dev;注意 getstillroom.com 已被一个环境音 app 占用)、GitHub org、npm/homebrew 包名核验;正式发布前做一次商标检索。CLI 命令名已随新仓库定为 `still`(`still distill` / `still status`)。README 开头用两句话讲 still room book 的典故——这个名字自带 About 页面。
