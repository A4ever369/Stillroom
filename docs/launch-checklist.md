# Open-Source Launch Checklist

Status of what stands between "the repo is public" (done) and "a stranger can
install it, trust it, and get value." Grouped by severity. Checked = done.

The core value proposition is **validated**: dry-run distillation across five
diverse real projects (a Next.js app, a Go security server, an LLM-gateway
billing fork, an agent-eval plugin, a courses platform) produced ~78 accurate,
durable facts and 4 real runbooks, with secret redaction firing on real
key-laden sessions and no visible leaks. What remains is mostly mechanical.

## Blockers — must fix before `go install` works for anyone

- [ ] **Module path ≠ repo URL.** `go.mod` says `github.com/0xbeekeeper/stillroom`
      but the repo is `github.com/A4ever369/Stillroom`, so
      `go install github.com/A4ever369/Stillroom/cmd/still@latest` fails and the
      import paths point at a module that isn't hosted where they say. Pick the
      permanent home (org + name) and rename the module to match. Do this first —
      everything else (binaries, docs, examples) bakes in the path.
- [ ] **No install path other than "clone + build".** Add a `go install`
      one-liner (fixed by the above) and prebuilt binaries via GoReleaser
      (darwin/linux/windows × amd64/arm64) attached to GitHub Releases. A
      Homebrew tap is a strong follow-up.

## Safety & correctness — before telling anyone to run it

- [x] **First-run cost guardrail.** `distill --limit N` processes the N most
      recent sessions; every run prints the pending count up front ("N session(s)
      to distill — each is a `claude -p` model call") and suggests `--limit` past
      a small threshold. (2026-07-22)
- [ ] **Codex distillation coherence.** Codex sessions are discovered, but
      distillation always shells out to `claude -p` (the only `Runner`). A
      Codex-only user still needs Claude Code installed. Either add a
      `CodexRunner` (headless `codex exec`) or state the dependency plainly in
      the README so it isn't a surprise.
- [ ] **Document the redaction boundary honestly.** Redaction is conservative,
      regex-based, and scrubs credential *shapes*, not confidential *content*.
      The knowledge base is committed and shared, so the PR review is the real
      checkpoint. Say so in the README and the PR-comment template — do not let
      users over-trust it.
- [x] **Distillation quality validated on real, diverse projects.** (2026-07-22)
- [x] **Secret redaction verified on real key-laden sessions.** (2026-07-22)

## Docs & language — all public surface in English

- [ ] **Translate the internal docs to English.** `docs/design-v2.md`,
      `docs/testing.md`, `docs/progress.md`, `docs/development.md` are currently
      in Chinese. For a public repo the whole surface should be English. (Code
      comments, README, and CLI help already are.)
- [ ] **`CONTRIBUTING.md`** — build/test/lint (`go test ./...`, `gofmt -l`,
      `go vet`, `bash scripts/smoke.sh`), the zero-dependency rule, the
      invariant-organized test layout, and how to add an adapter.
- [ ] **Show real output in the README.** A short before/after: a session digest
      in, the fact files + PR comment out. A GIF of `init → distill → review`.
- [x] **README, quickstart, privacy section, commands table.** (verified E2E)
- [x] **LICENSE (Apache-2.0).**

## Project hygiene

- [ ] **`CHANGELOG.md`** and semver tags starting at `v0.1.0`.
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
