# Traces Git — Design v2: Two-Plane Architecture and a Git Foundation

> Supersession: this document is the third after `session-ir-research-brief.md` (the research brief) and `architecture.md` (the v1 blueprint), and it **converges on an implementable design**. See §9 for how it differs from the first two.
>
> In one sentence: evidence is not fusible, knowledge is fusible — cut the system along that line into two planes, carry the knowledge plane directly on a real git repo so that the fusible unit coincides with git's fusible unit, and get merge / review / permissions / history all for free.
>
> **Companion document:** this one is the *architecture*. [`product-design.md`](product-design.md) is the *product* — users and jobs, the object model, the five surfaces, the consent constitution, activation, metrics, and the four horizons. Where the two overlap (positioning §11, the roadmap §8, the server plane §17), this document holds the reasoning and the product doc holds the user-facing consequences.

---

## 0. Restating the Problem

When a team collaborates, each person's conversations, experience, and history with their own coding agent (Claude Code / Codex / Cursor…) should be maintainable and transferable like a codebase: I can "push" my experience out, a teammate can "pull" it down and **fuse** it into their own context, then keep working with the AI carrying the fused memory. Not session overwrite — fusion.

The core tension: **conversations themselves are not mergeable** (two linear conversations have no meaningful merge result), but **what you learn from a conversation is mergeable**. The whole design unfolds from this one point.

## 1. Two-Plane Architecture

```
┌─ Evidence Plane ─────────────────────────────────────┐
│  Raw session transcripts (jsonl, etc.), append-only,  │
│  content-addressed, never merged, only referenced.    │
│  Value = replay / provenance / audit / fork seed /     │
│  digging into detail                                   │
└────────────────────┬─────────────────────────────────┘
                     │ distill (runs locally, produces a diff, human review)
                     ▼
┌─ Knowledge Plane ────────────────────────────────────┐
│  facts + playbooks + skills,                          │
│  small text files, stored in a real git repo          │
│  ("team knowledge base").                             │
│  Mergeable, reviewable, versionable, blameable.        │
│  This is what the team actually collaborates on.       │
└────────────────────┬─────────────────────────────────┘
                     │ materialize (render into a tool-injectable form)
                     ▼
        CLAUDE.md / AGENTS.md / memory/ / skills/ / MCP resources
                     │
                     ▼
     Any tool's new session naturally carries the team's fused memory
```

Design invariants:

1. **Fusion happens only in the knowledge plane.** The evidence plane only accumulates and is only referenced; it never participates in merge.
2. **Every piece of content in the knowledge plane must point back to the evidence plane** (provenance), but not the reverse — undistilled evidence is allowed to exist.
3. **The sharing boundary = the knowledge plane.** By default only distilled artifacts enter the team repo; raw transcripts stay on the local machine or in a private evidence store, and sharing is an explicit action.

## 2. Key Decision: Carry the Knowledge Plane on a Real Git Repo

**The fusible unit = git's fusible unit: one file per fact, one file per playbook topic.**

What this mapping buys us:

| Need | What git gives natively |
| --- | --- |
| Fusion: two people each learn different facts | Directory-level merge = set union, zero conflict |
| Conflict: one fact, two values | Same-file conflict → exactly the case that needs a human to adjudicate |
| Human confirmation / redaction preview before sharing | PR review flow, off the shelf |
| "Who learned this in which session" | `git blame` + frontmatter provenance |
| Rollback / history / branching / team permissions | Native git |

**Corollary**: the self-built merge engine, trace-level grant service, and ingest API from the v1 blueprint are all unnecessary at the MVP stage. They aren't rejected — they're deferred until the evidence plane and the enterprise form require them (§8 Phase 2+).

Honest trade-offs:

- Permission granularity = repo granularity; no trace-level grant. Early on, one knowledge base per team is enough; fine granularity is the reason to move to a server side in Phase 2.
- No semantic search early on. The materialized full text is already in the agent's context/workspace, and the agent grepping on its own covers most cases; pgvector retrieval belongs to the evidence-plane service (Phase 2).
- LLM-assisted playbook synthesis is a **non-deterministic** operation. We don't pretend it's a git-style deterministic merge: the synthesis result is stored as a new commit with full provenance, and we make no promise of reproducibility (see §5).

## 3. Knowledge Base Repo Layout

```
team-context/                      # team knowledge base (real git repo)
├── facts/
│   ├── deploy.acme.db-endpoint.md
│   ├── build.monorepo.pnpm-quirk.md
│   └── ...                        # one file per fact, filename = semantic key
├── playbooks/
│   ├── customer-onboarding-deploy.md
│   └── ...                        # one file per topic
├── skills/
│   └── ...                        # promoted org skills (standard SKILL.md structure)
├── evidence-index/
│   └── index.jsonl                # evidence pointers (trace ref → location), no transcript body
└── .tg/
    └── config.yaml                # materialize targets, redaction rules, scope mapping
```

### 3.1 Fact File

```markdown
---
id: deploy.acme.db-endpoint            # semantic key = fact identity, also the filename
scope: repo:acme-infra                 # applicable scope (repo / environment / global)
observed_at: 2026-07-18T09:30:00+09:00 # observation time — the carrier of recency semantics
source: trace://allen/a3f9c2/turns/41-58  # points back to the evidence plane
confidence: high                       # high | medium | low
status: active                         # active | superseded | disputed
supersedes: deploy.acme.db-endpoint@2026-05-02   # optional: the older observation it overrides
---
The entry to Acme's production database is pgbouncer, not a direct connection: port 6432.
A direct connection to 5432 is blocked by the security group.
```

Key points:

- **`observed_at` and `supersedes` are part of the fusion semantics, not metadata decoration.** Facts expire; a union without recency annotation just accumulates stale garbage.
- The materializer injects only `status: active` facts; `superseded` ones stay in history for traceability; `disputed` ones go into the review queue.
- The body is natural language, one fact says one thing, and its length is bounded by "can be injected standalone and is self-explanatory."

### 3.2 Playbook File

A playbook = a reusable recipe for a class of task ("customer onboarding deploy" in the Cuenca script is one playbook). Structure: preconditions → steps → known pitfalls (link related facts) → evidence links (source trace). A playbook is a higher-order product of distillation, usually first created from one successful session and revised by later sessions.

### 3.3 A Pragmatic Strategy for Fact Identity (Deduplication)

"Are these two memories the same fact" is a genuine research problem (see the research brief's open question 2); the MVP does not aim for a perfect solution:

1. The distilling LLM is responsible for proposing the semantic key (a `domain.object.attribute`-style slug), and reads the existing facts/ directory for key alignment before proposing;
2. At merge time, run near-duplicate detection with embedding similarity once; suspected duplicates are flagged into the PR for a human to adjudicate;
3. Key collides but values differ → git conflict → human adjudicates, and the adjudication itself becomes a commit (no re-adjudication, because git has a common ancestor).

## 4. Three Components to Write

With the foundation handed to git, the self-built surface converges to three things.

### 4.1 Distiller — the Core Moat, the Only AI Component

- Input: local session archives (Claude Code `~/.claude/projects/<encoded-cwd>/*.jsonl` + the memory directory; Codex `~/.codex/sessions/**`; the Cursor store).
- Output: **a diff against the knowledge base repo** — which fact files to add/update, which playbook to revise, with provenance pointers attached.
- Constraint: **must run locally** (transcripts don't leave the machine); the output is redacted (`redact.*` is reusable) and then submitted as a PR/commit.
- Human-in-the-loop: PR review is the redaction preview + quality gate. **Beware the intuition that "distillation redacts naturally" — it may be the opposite**: a raw transcript leaks secrets by accident, but a distilled fact is a deliberate condensation ("the prod credential is under key X in the vault" is exactly what distillation most wants to keep), so the distillation layer must pass through redact + human review, with neither omitted.
- Reuse on the parsing side: Multica already normalizes 15 runtimes into a unified turn shape (`agent.Message`), so the export side is ~80% off the shelf; the difference is that we need to newly write the at-rest static file parsing (tolerant + version-probing, no rigid parsing).

### 4.2 Materializer — Nearly Tool-Agnostic

The industry is converging on injectable-context standards: CLAUDE.md / AGENTS.md, the memory directory, skills, MCP resources. The materializer renders the knowledge base into this set of forms:

- Generate a marked CLAUDE.md/AGENTS.md block (filtering facts relevant to the current repo by `scope`);
- Write memory files and the skills directory;
- (Phase 2) supply dynamically as MCP resources.

**The cost asymmetry is an important dividend of this design: the import side is one-size-fits-all, and the per-tool grunt work is only on the export parsing side.** The adapter matrix is cut in half.

### 4.3 Evidence Store

- Transcripts are large (measured 10–40MB per session), so they don't go into git; put them in object storage (self-hosted MinIO is fine), content-addressed, with the `source:` pointer pointing into it.
- The MVP can be even thinner: first just register pointers in `evidence-index/index.jsonl` (machine + session id + content hash), leave the transcript on the original machine, and upload centrally later when replay/retrieval needs arise.
- Phase 2 builds on top: replay UI (reusing `buildTimeline()`), pgvector semantic search, MCP `search_traces / read_trace / fork_trace`.

## 5. Fusion Semantics (by Layer, Final Version)

| Layer | Fusion method | Conflict handling | Determinism |
| --- | --- | --- | --- |
| facts | File-level set union; same-source same-key overridden by `observed_at` (supersede) | Different-source same-key different-value → git conflict → human adjudicates | Deterministic (git) |
| playbooks | LLM synthesis/revision, produced as a new commit | Semantic contradictions flagged for a human | **Non-deterministic, fixed by the commit** |
| skills | Dedup by name, version increment | Version fork goes through a PR | Deterministic |
| transcripts (evidence) | **Never merged**; only referenced as provenance | none | — |
| code/artifacts | Real git merge (in the code repo) | git conflict flow | Deterministic |

The boundary of the analogy to git must be stated clearly: the magic of git merge lies in being deterministic, reproducible, and dumb, pushing all the semantic burden onto the human. The facts layer preserves this property; the playbooks layer cannot (LLM synthesis), so **we don't disguise it as a merge, but treat it as an agentic revision operation** whose result is committed and whose provenance is fully recorded (which source traces / which old versions were fed in).

## 6. The `tg` CLI Command Surface

```bash
tg init                  # associate the team knowledge base repo + local tool archive paths
tg distill [--session S] # local distill: session → knowledge base diff (with redaction), open a PR or local commit
tg push                  # = git push (syntactic sugar: push the knowledge base + upload newly registered evidence pointers)
tg pull                  # = git pull + auto materialize
tg materialize [--repo R]# knowledge base → CLAUDE.md block / memory / skills (filtered by scope)
tg status                # which local sessions are undistilled, how far behind the knowledge base is
tg evidence push <ref>   # explicitly upload a transcript to the evidence store (not uploaded by default)
```

Workflow (the Cuenca script, MVP version):

1. Allen finishes deploying a customer environment → `tg distill` locally generates a PR: 1 playbook + a few facts, all with provenance;
2. Allen glances at the redaction preview and merges the PR;
3. Cuenca runs `tg pull` (or her tool does it automatically via a hook) → the materializer injects the playbook and related facts into her CLAUDE.md/memory;
4. Her Claude Code follows the recipe to completion; when stuck (Phase 2) she uses `read_trace` to page through Allen's raw evidence.
5. **Allen is never interrupted; and in the MVP loop not a single self-built service is required — one CLI + one git repo.**

## 7. Load-Bearing Assumptions and Validation Order

**The first load-bearing assumption is "re-materialization works," not "fusion is feasible"**: injecting the distilled context into a new session really lets another person (their AI) pick up where the work left off. The value of fusion depends on it — if injection doesn't work, no matter how beautiful the merge, it's meaningless.

Validation order (an adjustment to the order in §9 of the research brief):

1. **Re-materialization PoC (first)**: take one real session → `tg distill` → materialize into a new session → have another person perform a similar task. Metrics: token consumption relative to a cold start, number of repeated explorations (re-Reading already-known files), task completion quality.
2. **Fusion PoC**: two people each work against the same environment → each distills → git merge the knowledge base → validate the three paths of union/override/conflict, and the acceptance criterion is task-level (does the fused memory let a third person work better), not "printing out the union effect."
3. Only after that do we talk about evidence store centralization, replay, semantic search, the MCP surface, and the multi-tool matrix.

## 8. Roadmap

- **Phase 1 (validate the core assumption, ~1–2 weeks in scale)**: the `tg` CLI (init/distill/materialize/pull/status) + Claude Code single-tool export + the team knowledge base repo + the PR workflow. Use our own team as the first user and run the Cuenca loop end to end. **The delivery form should be a Claude Code plugin first (hook + skill), not a standalone CLI** — see §13.
- **Phase 2 (turn the evidence plane into a service)**: centralized evidence store upload, replay UI, pgvector semantic search, MCP server (`search_traces / read_trace / fork_trace`), Codex/Cursor export. At this point the v1 blueprint's ingest API / tenant isolation / grant model are enabled as originally designed.
- **Phase 3 (enterprise form)**: SaaS multi-tenant hardening (RLS, quotas, billing), trace-level fine-grained grant, the org skill promotion pipeline (fact/playbook → skill, human-in-the-loop), a license gate.

## 9. Comparison with the First Two Documents

| Dimension | v1 blueprint / research brief | This design (v2) | Rationale |
| --- | --- | --- | --- |
| First foundation | Self-built central service (ingest + PG + grant) | Real git repo | merge/review/permissions/history for free; validate the assumption before building services |
| IR layering | SessionIR as one object with five layers | Split into two planes; the turns layer goes to the evidence plane | "IR is mergeable" holds only for the distillation layer; merging into one object would tempt merging the turns |
| Validation order | Fusion PoC first | Re-materialization PoC first | Fusion's value depends on re-materialization working; validate the load-bearing assumption first |
| fact primitive | A set of facts, no recency | + observed_at / supersedes / status / scope | Facts expire; a union without recency accumulates stale garbage |
| merge determinism | Undifferentiated | facts deterministic / playbooks explicitly non-deterministic | Preserve git's trust model while being honest about LLM synthesis |
| distillation and privacy | Distillation seen as natural redaction | Distillation seen as a **condenser**, must go through redact + human review | What distillation keeps is precisely the high-value sensitive information |
| adapter cost | export/import bidirectional, per tool | The import side is nearly tool-agnostic (CLAUDE.md/skills standards) | An industry-convergence dividend, the matrix cut in half |
| lineage | Single parent (ForkedFromTraceID) | git multi-parent commits natively supported | Merge is inherently multi-parent; avoids changing the schema later |

The v1 blueprint's moat building blocks (15-runtime normalization, redact, buildTimeline, the grant model, pgvector, the self-hosted channel) are all retained; only their enablement point is pushed back to Phase 2/3.

## 10. Open Questions (Left for Research, Continuing §7 of the Research Brief)

Retaining the research brief's 10 open questions, plus three literature anchors, to avoid groping from scratch:

1. Fact identity and deduplication ≈ entity resolution + truth discovery / knowledge fusion (the Google Knowledge Vault lineage), as well as the deduplication practices of agent memory systems (MemGPT/Letta, Zep Graphiti, Mem0);
2. The convergence semantics of the facts layer ≈ CRDT literature (grow-only set + tombstones + LWW); this design chooses the git three-way merge route, but if Phase 2 goes to server-side real-time fusion, CRDT is the fallback;
3. Re-materialization fidelity evaluation: task-continuation benchmarks (same-repo task pairs, cold start vs. injected distilled context), metrics = tokens to completion, redundant tool calls, correctness rate, and human rating of "does it feel like continuing the original author's work."

---

## 11. Strategic Positioning: General Infra, Not a Converter

> Positioning in one sentence: **a cross-tool team human-AI collaboration knowledge system-of-record**. The converter is the wedge and the acquisition entry point — it's a feature, not an identity.

### 11.1 Why the Neutral-Layer Position Holds

The mutual incompatibility of the big players is structural: Anthropic won't write a session parser for Codex, and OpenAI won't build memory import for Claude Code. Wherever the vendors are mutually exclusive is where the neutral layer can survive — precedents: Terraform for the various clouds, Plaid for banks, Segment for analytics tools, OpenRouter for model APIs. And mixing multiple tools is the norm for teams, while an individual's tool-switching cycle is measured in months: **the faster the tool churn, the greater the value of "knowledge travels with the person and the team, not with the tool."** The retention story is built precisely on the instability of the tool ecosystem.

### 11.2 Three-Tier Value Gradient (Resources and Narrative Are Allocated Along It)

1. **Adapters (the capture/conversion layer)**: grunt work, not hard work — replicable in two weeks, and standardization keeps flattening its value (AGENTS.md has already commoditized the injection side). → **Strategy: open-source it all**, proactively become the de facto standard, let the community maintain format drift (the most annoying part), and commoditize competitors' capture layers (commoditize the complement, the Terraform providers playbook).
2. **The knowledge plane (product core)**: the fact model, recency semantics, fusion, provenance, the PR review workflow. This is where the self-built moat is, the vehicle for commercialization.
3. **The knowledge corpus (the moat)**: more sessions → more accurate facts → new sessions more usable, compounding growth; and this asset **survives any single tool switch**. The system-of-record position is determined by it.

### 11.3 Market Boundary (the Honest Version)

What "the big players don't support each other" protects is the segment of **multi-tool teams**. If Anthropic builds good-enough native team memory for pure Claude Code teams, we can't enter the single-tool market. Our turf: mixed-tool teams + customers with a hard requirement for self-hosting / data staying inside the enterprise + teams that want git-ified, reviewable memory. Big enough, but recognize the boundary; see §12 for the competitive landscape.

### 11.4 Capture Durability (a Design Constraint)

Sessions are moving to the cloud (Claude Code on the web, Cursor cloud agents), and cloud sessions have no local jsonl, so at-rest parsing will gradually fall short. **The capture strategy doesn't bet on file parsing**: hook/SDK-level capture (e.g. the Claude Code SessionEnd hook provides `transcript_path`) is more durable than at-rest parsing. The adapter interface abstracts both sources into the same input: `capture source ∈ {at-rest file, hook/SDK stream}`.

## 12. Competitive Landscape (2026-07 Research Snapshot)

### 12.1 Demand Signals

- anthropics/claude-code [#38536](https://github.com/anthropics/claude-code/issues/38536): a structured "shared team memory" need raised by an engineering manager (memory pool, memory promotion, handoff context transfer, incident-response continuity), matching this design's scenarios point by point; [#40981](https://github.com/anthropics/claude-code/issues/40981) requests cross-member session sharing. Both are open, with no official response.
- Community self-help has already appeared: [claude-session-memory](https://github.com/teamspwk/claude-session-memory) (auto capture → knowledge card → git sharing, an isomorphic idea, toy stage), the session-share skill, etc.
- The existing workarounds listed in the issues all fail at the same point: **they require the user to add an action** (manually reconstruct context / export and paste into a ticket / verbal handoff / a static CLAUDE.md). → The design principle in §13 comes from this.

### 12.2 Player Comparison

| Player | What they did | What they lack |
| --- | --- | --- |
| Claude Code native | Sessions strictly local, per directory; at the team level only a static CLAUDE.md | No cross-user sharing, no team memory, no distillation |
| Cursor native | Memories (individual level); the only team-shareable thing is hand-written rules | Memories don't cross the team, no session distillation |
| GitHub Copilot Spaces | Team context spaces | Content relies on manual curation, feeds only Copilot, a single-tool island |
| Mem0 / Zep / Letta | Memory API infrastructure, with org-level scope | An API for developers building agents, not a team collaboration product; the open questions in their 2026 report (memory staleness, privacy consent, evolution vs. replacement) conversely argue for the necessity of this design's fact schema |
| **SpecStory (the closest)** | Local-first capture of 7 tools' sessions, cloud sync, Lore turns history into skills, 1.2k star | Capture/search-first; no fact model, no fusion semantics, no provenance chain; **team sharing marked "coming soon"** |

### 12.3 Window and Risk Assessment

- The first-mover window is measured in **quarters** (SpecStory's team features are on the roadmap).
- The biggest risk = platform nativization (community anticipation is strong enough that a fabricated "Claude Code team memory leak" article once appeared). The structural defense = the three things the platform vendors won't do: cross-tool neutrality, self-hosted data staying inside the enterprise, git-native.
- Directional tailwind: AGENTS.md has become the cross-tool de facto standard, and the Materializer's bet that "one import fits all" is redeemed by the industry's convergence.

## 13. A Zero-Friction User Flow (Fitting Existing Scenarios)

**There is only one design principle: zero new habits — every step parasitizes an action the team already performs.**

| Step | Parasitized host | Mechanism |
| --- | --- | --- |
| Capture | Install the plugin once | SessionEnd hook auto-distills in the background, no manual `tg distill` command |
| Review/redaction | The PR you already have to review | Native session↔PR association (`--from-pr`); the knowledge diff enters the **same PR** as an accompanying commit / bot comment |
| Storage | The existing code repo | The MVP doesn't build a separate knowledge base repo — it puts a `.team-context/` directory, so permissions/clone/CI all ride the existing car |
| Pull + materialize | `git pull` on the code | One line in CLAUDE.md, `@.team-context/materialized.md` import; a teammate pulling the code pulls the knowledge, and starting a session carries it automatically — this step **disappears entirely** |
| Onboarding | clone the repo | A newcomer starts Claude Code on day one with all the team's facts/playbooks already in context. Zero actions, a more universal demo scenario than the Cuenca script |

The user-visible surface converges to two things: **install a plugin once + read one extra knowledge diff in a PR**. Everything else is invisible. The `tg` CLI is kept for power users and debugging. The cross-repo org-level knowledge base, the MCP retrieval surface, etc. are introduced only in Phase 2.

## 14. Open-Source and Commercialization Strategy

**Open-source first; commercialization follows the Phase 2 server-side landing.** Three decisive reasons:

1. **Privacy trust**: the input is the whole company's AI session transcripts, and a closed-source SaaS can't pass the enterprise trust gate at the start; "open source + local-first + git-native" moves the server entirely out of the data path, and the MVP doesn't need a server anyway.
2. **Standard adoption**: §11's goal is to be the de facto standard, and a standard must be open; the direct competitor on the track (SpecStory, Apache-2.0) and the community solutions are all open source, so a closed-source one can barely even earn the right to be evaluated.
3. **The moat is not in the code**: the knowledge corpus compounds inside the customer's own repo, and the chargeable value is in the central service layer. What open-sourcing gives away is the part we couldn't have defended anyway.

**The dividing line in one sentence: what can run on a single machine + git is always open source and free; what needs a central service is paid.**

| | Open source (Apache/MIT) | Commercial (SaaS subscription / self-hosted license) |
| --- | --- | --- |
| Content | spec, adapters, tg CLI, plugin, distiller, materializer | centralized evidence store, replay UI, semantic search, MCP retrieval surface, cross-repo org knowledge base, trace-level permissions, SSO/audit |
| Corresponding phase | All of Phase 1 | All of Phase 2/3 |

License tiering: use permissive licenses for the spec and adapters to maximize adoption; server-side components can later consider AGPL/BSL to prevent cloud vendors from free-riding (decide in Phase 2, don't spend energy on it now).

**Launch actions** (open source ≠ dumping it on GitHub; the precedent of an isomorphic idea with 0 stars is right in front of us): Show HN; reply directly on anthropics/claude-code #38536 and #40981 — the first batch of target users have already drawn their own profiles by name in the comments; pair with the AGENTS.md niche to write a positioning piece titled "Your team's memory shouldn't be locked into one tool."

## 15. Development Plan

- **M0 skeleton (done)**: `internal/ir` (fact/playbook model, frontmatter encode/decode, supersession semantics, store), `internal/redact` (secret redaction), `internal/parser` (digest: transcript → distillation input, with metadata extraction and head/tail truncation), `internal/distill` (prompt building, local distillation via `claude -p`, second-pass redaction, proposal validation), `internal/materialize` (deterministic rendering + CLAUDE.md import), `cmd/tg` (init/distill/materialize/status/hook), the Claude Code plugin (SessionEnd enqueue). All with unit tests, end-to-end smoke passing.
- **M1 dogfooding (this week)**: run distillation on our own team's real sessions, iterate on prompt quality (this is where the core moat lies); validate the re-materialization assumption — with the distilled context injected into a new session, can another person pick up the work (the first load-bearing assumption in §7); tune fact granularity and playbook structure based on results.
- **M2 open-source release**: split out a standalone open-source repo (or make this repo public), LICENSE, README, install docs, demo GIF; execute the §14 launch actions; collect the first batch of feedback from multi-tool teams.
- **M3 fusion validation**: two people each distill against the same environment → git merge the knowledge base, validating the three paths of union/override/conflict; a near-duplicate detection bot with embedding similarity (flag suspected synonymous facts on the PR); task-level evaluation (cold start vs. injected, tokens/redundant exploration/correctness rate).
- **M4 Phase 2 server side**: per §8, the centralized evidence store, replay, retrieval, MCP surface; commercialization starts alongside.

The evolution path of distillation automation: the MVP is "SessionEnd enqueue + manual `tg distill`" (no spending tokens without consent, no background LLM calls); after trust is validated, add an opt-in auto-distillation mode (background `claude -p --no-session-persistence`, with hook-recursion protection built in).

## 16. Naming: Stillroom

**The product name is Stillroom**; Traces Git is demoted to an internal codename / repo name that we keep using.

Origin and the point where it clicks: still = the still (the root of distill); a stillroom, from the 16th century, was a dedicated room in English manors for distilling medicinal spirits and refining herbs, whose mistress kept, generation after generation, a **still room book** — recipes, remedies, and experience entries, passed from one generation to the next, each adding their own newly verified entries. It is a real, historical cross-generational team knowledge base. One word hits both cores of the product at once: **distill (distill sessions) + a shared handbook passed down through generations (.team-context)**.

Naming research conclusion (2026-07): Engram (already raised $98M), Tacit, Cairn, Baton, Slipstream, Kindling, Stigmerge, and other candidates are all taken in the AI/dev tool space; Stillroom is essentially clean in that space (only distant small brands in the aromatherapy/journaling category exist).

To do: register the domain (candidates stillroom.dev / stillroom.ai / getstillroom.dev; note that getstillroom.com is already taken by an ambient-sound app), the GitHub org, verify the npm/homebrew package names; run a trademark search before the formal release. The CLI command name has been set to `still` along with the new repo (`still distill` / `still status`). Open the README with two sentences telling the still room book anecdote — this name comes with its own About page.

## 17. The Server Plane: `stillroomd`

Phase 2 (§8, §14) is where a central service earns money, and it is also where
this kind of product usually dies — an internal tool that demands a database, a
backup policy and a security review does not get deployed. One decision avoids
all of that:

> **The server owns no source of truth.**

`stillroomd` indexes `.team-context/` directories out of git checkouts and
serves search over them. Every document it holds is derived. The index is a
cache with no authoritative copy of anything.

### 17.1 What that buys

| | Because the server holds only derived data |
| --- | --- |
| Deployment | one static binary, no database, no migrations |
| Backup | nothing to back up; delete the volume, it rebuilds from git |
| Security review | a compromise exposes nothing that was not already in the git host |
| Exit cost | stop the container; not one byte of knowledge is lost |

The last row is a *sales* argument, not an engineering one. "If it is useless,
delete the container and you lose nothing" removes the largest single objection
to adopting an internal tool.

### 17.2 Invariants (enforced by tests, not by convention)

1. **The server never reads the evidence plane.** Transcripts stay on the
   machine that produced them; `.team-context/queue/` is machine-private and is
   never indexed. A fact's `source` field is a *citation*, not a link.
2. **The server never writes to a repo.** Editing knowledge server-side would
   route around the PR review that makes the knowledge trustworthy (§13).
3. **The CLI never requires the server.** The moment any core workflow depends
   on a central service, the local-first trust argument of §14 collapses.

### 17.3 Permissions: delegate, do not invent

The target model is *if you can read the repo, you can read the repo's
knowledge* — answered by asking the git host, not by a permission system of our
own. A second, divergent copy of an org's repo ACLs is a liability. The only
model that genuinely needs to be built is per-person activity visibility
(§17.5), and that one is opt-in by construction.

### 17.4 Build order, and why

1. **Cross-repo search** (shipped). The only feature with *no* political
   surface: nobody objects to finding the pitfall another team already hit.
2. **Knowledge health.** Staleness, cross-repo contradictions, orphaned
   knowledge. This is the retention feature — a knowledge base dies of rot, not
   of scarcity — and no adjacent product does it.
3. **Personal activity, consent-first** (§17.5).
4. **Agent audit and eval.** Aggregate, agent-subject reporting; continuous
   distillation-quality eval against `eval/baseline.json`.

Search before activity is deliberate. Search is uncontested value; activity is
contested value. Shipping them in the wrong order spends the trust needed for
everything else.

### 17.5 The activity feature, and the line it must not cross

A per-person view of "what did they do today", derived from their sessions, is
the highest-value and highest-risk thing in the roadmap.

- ❌ A manager opening a report's page to see their day is employee
  surveillance. It ends adoption of the whole product, permanently.
- ✅ A summary the person generated, reviewed, edited and chose to publish is an
  asynchronous standup, and people want it.

So the order is fixed: `still standup` (local, produces *my* draft, I decide
whether to publish) ships **before** any team-wide activity view, and the
server only ever receives what a person explicitly published. Any view whose
subject is a *person* requires that person's consent; views whose subject is an
*agent* ("41 agent sessions touched production config this week") do not, and
are the form the compliance demand should be answered in.
