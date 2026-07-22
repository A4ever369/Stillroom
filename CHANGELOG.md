# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and versions aim for
[Semantic Versioning](https://semver.org/). Until `v1.0.0` the `.team-context/`
on-disk format may still change; `still init` upgrades an existing layout in
place.

## [Unreleased]

### Added
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
- Go module path is `github.com/A4ever369/Stillroom` (matches the repo, so
  `go install …@latest` works).
- `BuildPrompt` excludes session/tooling meta and local-machine environment
  facts (OS, what's on PATH, ports other local apps hold) from team knowledge.

### Validated
- Distillation quality across five diverse real projects; secret redaction
  fired on real key-laden sessions with no visible leaks.
