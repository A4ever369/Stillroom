# Contributing to Stillroom

Thanks for helping. Stillroom is small, stdlib-only Go with a deliberately tight
set of rules — most of them are correctness invariants, not style.

## Build, test, lint

```bash
go build ./...            # or: make still  → bin/still
go test ./...             # must stay green
gofmt -l .                # must print nothing
go vet ./...
bash scripts/smoke.sh     # end-to-end scenario matrix, fake claude, no tokens
```

CI runs exactly these on every push/PR. The nightly workflow fuzzes the parsers;
`make eval` is the token-spending distillation-quality check (not in CI — run it
before and after any `BuildPrompt` change and compare against `eval/baseline.json`).

## Hard rules (please don't break these)

- **Zero dependencies.** Stdlib only. The tiny frontmatter parser in
  `internal/ir` is deliberate — do not add a YAML library.
- **Privacy.** Transcripts never leave the machine; distiller output is redacted
  before it touches disk; the hook never spends tokens.
- **Determinism.** `Fact`/`Playbook` `Encode()` and `materialize` output must be
  byte-stable — repeated runs may not produce a git diff.
- **Tolerant parsing.** Transcript formats drift across tool versions; skip a
  malformed line, never fail a whole file.
- **Hook contract.** `still hook …` must exit 0 silently on any problem — it may
  never break a user's session.
- **Supersession moves forward only.** A newer `observed_at` replaces; an older
  one never clobbers.

## Tests are organized by invariant, not by package

See `docs/testing.md`. When you fix a bug, add the test at the layer that would
have caught it (L1 unit, L2 invariant/fuzz, L3 CLI black-box, L4 end-to-end).

## Adding a source-tool adapter

An adapter lives in `internal/adapter/<tool>` and does exactly one thing: parse
that tool's at-rest transcript format into a `session.Digest`. Everything
downstream (redaction, distillation, fact stamping) is written against that one
type, so a new tool touches no pipeline code. Use `internal/adapter/codex` as
the template — build it against **real** transcript files, not a guess at the
format, and add a fuzz target for the tolerant-parsing invariant.

## Commits & PRs

- Keep the knowledge diff honest: if you change `BuildPrompt`, say what the eval
  scores did.
- Conventional-ish prefixes help the release changelog: `feat:`, `docs:`,
  `test:`, `chore:`.
- Append a line to `docs/progress.md` when you complete a milestone or make a
  direction-level decision.
