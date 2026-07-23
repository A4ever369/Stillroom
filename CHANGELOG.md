# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and versions aim for
[Semantic Versioning](https://semver.org/). Until `v1.0.0` the `.team-context/`
on-disk format may still change; `still init` upgrades an existing layout in
place.

## [Unreleased]

### Added
- **`stillroomd`** — a self-hostable server giving a team one search box over
  every repo's distilled knowledge. A single static binary with **no database**:
  it indexes `.team-context/` directories from git checkouts (`-scan DIR` /
  `-repo NAME=PATH`), so nothing needs backing up and stopping the container
  loses no knowledge. Ships a search UI, per-document pages with cross-repo
  "related" results, a `/api/search` JSON API for CI and agents, `/healthz`, and
  a distroless `Dockerfile`. It never reads session transcripts and never writes
  to a repo (`docs/self-hosting.md`, design §17).
- **`DESIGN.md`** — Stillroom's visual system (near-black canvas, four-step surface ladder,
  a single lavender accent, no shadows). The `stillroomd` UI is built on its tokens; any
  future UI follows it. Adapted from Linear's published design language, with a documented
  light theme and one state colour added.
- `internal/index`: read-only, in-memory search over many knowledge planes, with
  CJK support via character bigrams (no segmenter, zero dependencies).
- Codex CLI adapter: `still distill` discovers and digests
  `~/.codex/sessions/**/rollout-*.jsonl` alongside Claude Code sessions. The
  digest type is now tool-agnostic (`internal/session`).
- `still review --base DIR`: a semantic knowledge diff (by fact ID) rendered as
  a PR comment, plus `.github/workflows/knowledge-diff.yml` to post it.
- `still materialize --check`: verify `materialized.md` is current without
  writing (exit 1 if stale); `still doctor` reports the same drift.
- `still status --json`: machine-readable knowledge-base overview.
- `still distill --limit N` and an up-front cost heads-up: cap a run to the N
  most recent sessions so a first run over weeks of history can't fire an
  unbounded number of paid model calls.
- `still version`.
- Distillation-quality eval harness (`make eval`) with a committed baseline.

### Changed
- Near-duplicate detection now uses **idf-weighted token Jaccard plus an id
  token-set check**, replacing rune-bigram Jaccard. Tuned against 210 facts
  distilled from 21 real sessions: the old signal flagged 3 pairs over that
  corpus and missed hand-verified duplicates, because character bigrams cannot
  separate unrelated English technical prose from real duplicates. The id check
  catches word-order variants (`a.delete-env.cascade` vs `a.env-delete.cascade`)
  with no false positives — a shape the model produces often, since it
  re-derives an id every session.
- The tokenizer (words for Latin, character bigrams for CJK) moved to
  `internal/text` so search ranking and duplicate detection cannot drift apart.
- Go module path is `github.com/A4ever369/Stillroom` (matches the repo, so
  `go install …@latest` works).
- `BuildPrompt` excludes session/tooling meta and local-machine environment
  facts (OS, what's on PATH, ports other local apps hold) from team knowledge.

### Validated
- Distillation quality across five diverse real projects; secret redaction
  fired on real key-laden sessions with no visible leaks.
