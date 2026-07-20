# Stillroom — Claude Code plugin

> A *stillroom* was the room in a great house where knowledge was distilled —
> and the still room book was the household's knowledge base, maintained and
> handed down across generations. This is that, for your team's AI sessions.

Enqueues every finished Claude Code session for distillation into your repo's
`.team-context/` knowledge base.

## How it works

- On `SessionEnd`, the hook calls `tg hook session-end`, which records the
  transcript path in `.team-context/queue/` — **nothing leaves your machine
  and no LLM call happens automatically**.
- You run `still distill` when convenient (typically right before opening a PR).
  Distillation runs locally through your own `claude -p`, proposes facts and
  playbooks as plain files, and the diff rides your normal PR review.
- Repos that don't contain `.team-context/` are ignored entirely: the hook is
  a silent no-op until someone runs `still init` in that repo.

## Install

1. Build and install the CLI: `go install ./cmd/still` (must be on PATH).
2. Install this plugin directory via your marketplace, or for local use:
   `claude plugin install <path-to>/plugin/claude-code`
3. In each repo that should accumulate team knowledge: `still init`, commit the
   `.team-context/` scaffold and the CLAUDE.md import line.

## Why enqueue instead of auto-distill?

Deliberate MVP choice: an automatic background `claude -p` call at session end
would spend tokens without consent and could surprise users. The queue keeps
capture zero-effort while leaving the LLM spend and the share decision
explicit. An opt-in auto mode is on the roadmap (design-v2 §13).
