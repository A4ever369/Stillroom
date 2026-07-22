# Stillroom

[![CI](https://github.com/A4ever369/Stillroom/actions/workflows/ci.yml/badge.svg)](https://github.com/A4ever369/Stillroom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

**Distill your team's AI coding sessions into a shared, git-native knowledge base.**

A *stillroom* was the room in a great house where remedies and essences were
distilled — and the **still room book** was the household's knowledge base:
recipes and hard-won know-how, maintained and handed down across generations,
each keeper adding what they had verified themselves.

This is that, for the AI era. Your team works with Claude Code, Codex and
Cursor every day. Everything learned in those sessions — the infrastructure
quirks, the decisions, the pitfalls hit and resolved — evaporates when the
session ends. Stillroom captures it, distills it into reviewable **facts** and
**playbooks**, and puts them where every teammate's agent reads them
automatically. Knowledge follows the team, not the tool.

## How it works

```
  your Claude Code session ends
        │  (plugin hook enqueues the transcript — nothing leaves your machine)
        ▼
  still distill        ← runs locally through your own `claude -p`
        │  proposes facts + playbooks as plain markdown files
        ▼
  .team-context/       ← lives inside your repo; the diff rides your normal PR
  ├── facts/           one fact per file → git merge IS the fusion algorithm
  ├── playbooks/       reusable recipes distilled from successful sessions
  └── materialized.md  auto-rendered, imported by CLAUDE.md / AGENTS.md
        │  a PR bot comments the knowledge diff in plain language (review parasite)
        ▼
  every teammate's next session starts with the team's accumulated knowledge
```

Two design rules make this work:

1. **Evidence vs knowledge.** Raw transcripts are evidence — append-only,
   never merged, only referenced. Fusion happens in the distilled layer,
   where it is actually meaningful.
2. **The mergeable unit is git's mergeable unit.** One fact per file means
   two people learning different things merge cleanly, and two people
   learning *conflicting* things produce a git conflict — exactly the case
   that deserves a human decision.

## Quickstart

```bash
# install (or grab a prebuilt binary from the Releases page)
go install github.com/A4ever369/Stillroom/cmd/still@latest   # or: git clone && make still

# in the repo that should accumulate team knowledge
still init          # creates .team-context/, wires CLAUDE.md import

# install the Claude Code plugin (enqueues sessions automatically)
claude plugin install ./plugin/claude-code

# after a work session — typically right before opening your PR
still distill       # local LLM call via your own claude -p; writes fact files
git diff .team-context/   # review what would be shared, then commit
```

Teammates get the knowledge by pulling the repo. Nothing else to run:
materialized context is imported by `CLAUDE.md`, so their next session
simply starts smarter.

`still distill` discovers this repo's sessions from **both Claude Code and
Codex** automatically — a second tool is just a second adapter, and the
distilled knowledge is tool-agnostic once it lands.

> **Note:** distillation itself currently runs through `claude -p`, so the
> Claude Code CLI must be installed and authenticated even to distill Codex
> sessions. A native Codex runner is on the roadmap.

## Reviewing knowledge in the PR

Knowledge changes don't need a separate review surface — they ride your
normal PR. Drop [`.github/workflows/knowledge-diff.yml`](.github/workflows/knowledge-diff.yml)
into a repo and, whenever a PR touches `.team-context/facts` or `/playbooks`,
a bot comments a plain-language diff:

```
### 🧠 Team knowledge changes
Facts: ➕ 1 new · ✏️ 0 updated · ➖ 0 removed

#### ➕ New facts
- ci.postgres.image (high): CI's Postgres service must use pgvector/pgvector:pg16 …
```

The diff is semantic (by fact ID, not text): a no-op rewrite shows nothing, and
a fact whose observation advanced is flagged as a supersession. It runs on
first-party GitHub actions only — no third-party dependencies.

## A fact file

```markdown
---
id: deploy.acme.db-endpoint
scope: repo:acme-infra
observed_at: 2026-07-18T09:30:00+09:00
source: claude-code://a3f9c2
confidence: high
status: active
---
Acme production DB is reached via pgbouncer on 6432 — direct 5432
is blocked by the security group.
```

`observed_at` gives facts temporal validity: a newer observation of the same
key supersedes the old one instead of piling up stale knowledge. Only
`active` facts are materialized; history stays in git.

## Privacy

- Transcripts **never leave your machine**. Distillation runs locally through
  your own `claude -p`.
- Secret-shaped strings are scrubbed twice: before the transcript digest
  enters the prompt, and again on the distiller's output.
- Nothing is shared without an explicit `git commit` — the PR review is the
  human checkpoint, in the tool your team already uses.
- The plugin only enqueues; it never spends tokens or calls a model on its own.

## Commands

| Command | What it does |
| --- | --- |
| `still init` | set up `.team-context/` in the current repo |
| `still doctor` | check the whole setup end to end |
| `still distill` | distill queued **and auto-discovered** sessions (a local ledger prevents re-distilling) |
| `still distill --transcript PATH` | distill one transcript file, or **every `.jsonl` under a folder** in one run |
| `still distill --dry-run` | preview proposals without writing (combine with any of the above) |
| `still distill --force` | re-distill sessions the ledger already saw |
| `still distill --limit N` | distill at most N sessions, newest first (caps first-run cost) |
| `still materialize` | re-render `materialized.md` |
| `still materialize --check` | verify `materialized.md` is current without writing — exit 1 if stale (CI-friendly) |
| `still review --base DIR` | render a plain-language knowledge diff vs another checkout (used by the PR bot) |
| `still status` / `--json` | knowledge base, queue and discovery overview (`--json` for tooling/CI) |

The plugin is optional: `still distill` also discovers this repo's past
sessions directly from Claude Code's local storage, so your first
distillation can mine work you did before installing anything.

## Status

Early, but real. **Claude Code and Codex** are both supported today
(`internal/adapter/` is built so a new tool is just a new adapter); Cursor is
next. The knowledge model, the two-clone fusion, the redaction boundary and the
PR-comment loop are all in place and covered by an invariant-organized test
suite (`docs/testing.md`). The design doc — layered IR, fusion semantics,
roadmap — lives in [`docs/design-v2.md`](docs/design-v2.md).

## License

Apache-2.0
