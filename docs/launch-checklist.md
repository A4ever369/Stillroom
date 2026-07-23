# Open-Source Launch Checklist

Status of what stands between "the repo is public" (done) and "a stranger can
install it, trust it, and get value." Grouped by severity. Checked = done.

The core value proposition is **validated**: dry-run distillation across five
diverse real projects (a Next.js app, a Go security server, an LLM-gateway
billing fork, an agent-eval plugin, a courses platform) produced ~78 accurate,
durable facts and 4 real runbooks, with secret redaction firing on real
key-laden sessions and no visible leaks. What remains is mostly mechanical.

## Blockers — must fix before `go install` works for anyone

- [x] **Module path matches repo URL.** `go.mod` and all imports are
      `github.com/A4ever369/Stillroom`, so `go install
      github.com/A4ever369/Stillroom/cmd/still@latest` resolves once a tag is
      pushed. (2026-07-22)
- [x] **Install path.** `go install github.com/A4ever369/Stillroom/cmd/still@latest`
      works. GoReleaser (`.goreleaser.yaml`) + a tag-triggered workflow
      (`.github/workflows/release.yml`) cross-build darwin/linux/windows ×
      amd64/arm64 archives into a **draft** GitHub Release. Releasing is one
      deliberate step: `git tag v0.1.0 && git push origin v0.1.0`. (2026-07-22)
      A Homebrew tap is a good follow-up.

## Safety & correctness — before telling anyone to run it

- [x] **First-run cost guardrail.** `distill --limit N` processes the N most
      recent sessions; every run prints the pending count up front ("N session(s)
      to distill — each is a `claude -p` model call") and suggests `--limit` past
      a small threshold. (2026-07-22)
- [~] **Codex distillation coherence.** Codex sessions are discovered, but
      distillation always shells out to `claude -p` (the only `Runner`), so a
      Codex-only user still needs Claude Code. Documented plainly in the README
      for now (2026-07-22); a native `CodexRunner` (headless `codex exec`)
      remains a roadmap follow-up.
- [x] **Document the redaction boundary honestly.** (2026-07-23) Redaction is
      conservative, regex-based, and scrubs credential *shapes*, not confidential
      *content*; the PR review is the real checkpoint. Stated as rule R4 of the
      consent constitution in [`product-design.md`](product-design.md) §6 —
      including the reason it matters, that over-trusting redaction is worse than
      having none because it removes the reader's vigilance — and no surface may
      imply otherwise.
- [x] **Distillation quality validated on real, diverse projects.** (2026-07-22)
- [x] **Secret redaction verified on real key-laden sessions.** (2026-07-22)

## Docs & language — all public surface in English

- [x] **Internal docs translated to English.** `docs/design-v2.md`,
      `docs/testing.md`, `docs/progress.md` and `CLAUDE.md` are now English
      (`development.md` already was). The prose surface is fully English. (2026-07-22)
      Note: some `_test.go` fixtures keep CJK on purpose — they verify
      CJK/multibyte handling (the near-dup detector, rune-boundary clipping,
      redaction inside CJK context), a first-class use case, not documentation.
- [x] **`CONTRIBUTING.md`** — build/test/lint, the hard rules, the
      invariant-organized test layout, and how to add an adapter. (2026-07-22)
- [~] **Show real output in the README.** (2026-07-23) A verbatim before/after
      is in place — session turns in, the distill run, the fact file, the PR
      comment, and the point where it disappears into the teammate's next
      session. The input ships in `testdata/corpus/` so a reader can run it.
      Still missing: a GIF of `init → distill → review`.
- [x] **README, quickstart, privacy section, commands table.** (verified E2E)
- [x] **LICENSE (Apache-2.0).**

## Project hygiene

- [x] **`CHANGELOG.md`** (Unreleased section ready). [ ] Cut the first semver
      tag `v0.1.0` when ready — that fires the release workflow.
- [ ] **Freeze / version the `.team-context/` format.** Early adopters accumulate
      real knowledge; a breaking layout change must be an explicit, migrated
      upgrade (the `init` in-place upgrade path is a good start — document its
      guarantees).
- [ ] **Cross-platform.** CI cross-compiles (L0), but the Windows plugin hook
      (`still hook session-end`) and shell integration are untested. Verify or
      scope the first release to macOS/Linux explicitly.
- [ ] Issue/PR templates, `CODE_OF_CONDUCT.md`, a `SECURITY.md` (how to report a
      redaction miss).
- [x] CI: gofmt/vet/test/smoke on every push; nightly fuzz. (`.github/workflows/`)

## Positioning — before Show HN / wide announce

- [ ] **A sharp README hook** and a one-paragraph "why this vs. a wiki / vs.
      pasting into CLAUDE.md by hand" comparison.
- [ ] **A landing** (stillroom.dev / .ai) — optional but helps.
- [ ] **A committed eval baseline** so quality regressions are visible when the
      prompt changes (`eval/baseline.json`, this batch).
- [ ] Show HN draft; replies to the relevant claude-code issues.

## Suggested order

1. Rename the module to the permanent repo path (unblocks everything).
2. `distill --limit` + first-run heads-up (safety before anyone runs it).
3. GoReleaser + `go install` path (distribution).
4. Translate docs to English; add CONTRIBUTING, CHANGELOG, real-output examples.
5. Decide the Codex-runner vs document-the-dependency question.
6. Positioning + Show HN.
