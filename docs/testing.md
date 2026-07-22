# Stillroom Test Plan

> Principle: **hard rules are the spec**. Those six invariants in CLAUDE.md are not
> style suggestions — they are this project's definition of correctness. Tests are
> organized by invariant, not by package: one invariant maps to one layer of
> executable evidence.

## Why full automation is possible

There is no GUI, no network dependency, no concurrent scheduling. The system's
entire surface is:

| Surface | Form | Testability |
| --- | --- | --- |
| Parse transcript / frontmatter | Pure function `[]byte → struct` | unit + fuzz |
| Knowledge rendering | Pure function `struct → []byte` | golden + property |
| Filesystem effects | Fully isolatable within a temp dir | black-box CLI |
| Model invocation | `Runner` interface, injectable | fake runner, zero tokens |
| Claude Code storage | `CLAUDE_CONFIG_DIR` redirectable | synthetic corpus |

The only thing that cannot be fully automated is **distillation quality** (how good
the model output is); that layer is isolated on its own into L5, made semi-automatic
via a gold-standard corpus, and kept out of CI.

## Layers

### L0 static gates (seconds, every push)

- `gofmt -l .` is empty
- `go vet ./...`
- `go build` cross-platform: linux/amd64, darwin/arm64, windows/amd64 (cross-compile
  only, no need to run)
- **Zero-dependency assertion**: the test reads `go.mod` directly and asserts there is
  no `require` block. Right now this hard rule relies only on human discipline; it
  should be guarded by a machine.

### L1 unit tests (existing, filled out)

**Goal achieved**: every internal package ≥ 85% — redact 100%, queue 92%, ledger 89%,
ir 89%, materialize 87%, claudecode 86%, distill 85%. The fill-out focus is on `ir`
(store error paths: `Exists` / `LoadPlaybooks` / `WritePlaybook` / `ensureLines`
upgrade, `SortFacts`) and `distill` (`BuildPrompt` injection branches, `Run` error
propagation, `Apply` write failure), plus queue error branches and materialize's
bad-file warning / import-append branches.

`cmd/still`'s `-cover` shows 0% because the black-box tests run the compiled binary as
a subprocess, which `go test -cover` cannot measure — its logic is actually covered by
L3 (`harness_test.go` and friends), it just isn't counted.

### L2 invariant tests (the core of this plan)

This layer is new: one invariant per test file, with failure messages pointing
directly at the violated hard rule.

| Invariant | Test form | Assertion |
| --- | --- | --- |
| **Determinism** | property test | random fact set → two `Encode()` calls are byte-identical; shuffling write order does not affect `materialized.md`; after two back-to-back materialize runs, `git diff` is empty |
| **Supersession is one-way** | property test | a random `observed_at` sequence written in any order converges to a terminal state that always equals "the latest observation"; an older observation never overwrites a newer one |
| **Privacy** | corpus-driven | a corpus of secret shapes (AWS key / JWT / PEM / `password = "..."` / bearer token / a key inside Chinese context) is entirely scrubbed by `redact`; and after injecting a secret via the fake runner, assert that **no file on disk** contains the original text |
| **Tolerant parsing** | fuzz | `FuzzDigestSession`, `FuzzParseFact`, `FuzzParseProposal`: no input panics; half-corrupt jsonl still yields the good lines before it (no whole-file failure) |
| **hook contract** | table-driven black-box | a dozen kinds of bad input (empty stdin / non-JSON / missing fields / huge payload / nonexistent cwd / uninitialized repo / unknown hook name) all **exit 0 with empty stdout+stderr** |

> Known violation right now: `still hook bogus` goes through `return fmt.Errorf(...)` →
> exit 1. The hook-contract test will go red the moment it lands — that is exactly the
> value of this layer.

### L3 CLI black-box tests (filling the 0% across 428 lines)

The test compiles the binary once, and each case gets its own isolated world:

```
tmp/
  repo/            fake project run through git init (cwd)
  claude-home/     CLAUDE_CONFIG_DIR, holds synthetic transcripts
  bin/claude       fake claude, emits different JSON per case
```

A table-driven run over the command matrix asserts exit code + stdout snapshot +
filesystem terminal state:

- `init`: brand-new repo / re-run (idempotent) / existing CLAUDE.md (append, do not
  overwrite) / non-git directory (error, exit 1)
- `distill`: no session / too-short session (minTurns) / normal / `--dry-run` writes
  nothing to disk / `--force` re-distills / `--transcript` specified / fake runner
  returns garbage JSON / fake runner times out
- `status`: empty store / has a bad file (reports BAD but does not crash)
- `doctor`: output and exit code when each of the six checks fails
- `materialize`: empty store / only an archived fact
- `hook`: see L2

The fake claude is a parameterizable script (reads env vars to decide what to emit),
so branches like "the model returns malformed output" become testable too — a large
area that is currently entirely uncovered.

### L4 end-to-end scenarios — **matrix landed**

`scripts/smoke.sh` has grown from a single happy path into a **scenario matrix** (pure
bash + fake claude, zero tokens). Each scenario gets its own isolated world (temp repo
+ `CLAUDE_CONFIG_DIR`/`CODEX_HOME` + fake claude), so failures are localized and
scenarios do not leak state into one another. It runs the **compiled real binary** and
tests the exact path a user experiences (shell integration, plugin hook, real
filesystem):

1. **Cold-start full flow** ✅: init → doctor → auto-discovery → distill (with
   redaction assertion) → materialize
2. **hook enqueue path** ✅: `still hook session-end` reads `{transcript_path,cwd}` and
   enqueues → the queue file appears → `status` shows pending 1 → distill consumes it →
   the queue empties, pending 0. (The transcript is deliberately placed **outside** the
   discovery directory to ensure the only entry point is the queue.)
3. **Idempotency and force** ✅: a re-run reports "nothing to distill"; `--force`
   re-distills
4. **Codex discovery** (new) ✅: CODEX_HOME holds a rollout whose cwd matches this repo,
   with an empty CLAUDE_CONFIG_DIR → distill discovers and distills it end-to-end,
   verifying the multi-tool wiring works through the real binary.
5. **Fusion** (`cmd/still/fusion_test.go`, Go black-box): two clones each distill →
   `git merge`. Verifies the core bet of design-v2 §2, "one fact, one file".
   **Conclusion: the bet holds** — disjoint knowledge is merged automatically by git;
   a genuine divergence (both sides edit the same fact) still stops and waits for human
   arbitration, with the conflict strictly confined to that one file while all other
   knowledge merges as usual.
   > This test also forced out the inevitable conflict on the generated artifact
   > `materialized.md` (see the changelog), and the newcomer-onboarding breakage caused
   > by empty directories not being tracked by git.
6. **Upgrade path** (new) ✅: hand-build an old-version `.team-context/` layout (has a
   fact, `.gitignore` missing `.local/`, no `.gitattributes`, no `.gitkeep`) →
   `still init` upgrades in place → assert the union-merge property / `.local/` / the
   two `.gitkeep` files are filled in, and that **existing facts are not lost**.
7. **review diff** (new) ✅: `still review --base/--head` renders a semantic knowledge
   diff; assert the anchor + new fact appear and unchanged facts do not.

### L5 distillation-quality evaluation (semi-automatic, not in CI) — **skeleton landed**

The only layer that needs real tokens, run separately via `make eval` (`cmd/eval`, not
a `_test.go`, so `go test ./...` / CI never triggers it; but `go build ./...` compiles
it to prevent bit rot). Mechanism:

- Each case is a pair of files under `testdata/corpus/<name>/`: `transcript.jsonl` (a
  real, redacted session) + `expected.md` (topic-level annotation of "what should have
  been learned", including negative controls for "what should not be fabricated"). See
  `testdata/corpus/README.md` for the format.
- Reuse the **production pipeline**: `DigestSession → distill.Run(ClaudeRunner)`
  produces a proposal, then a second `claude -p` call acts as an LLM-judge, scoring the
  proposal on three axes (0–5): recall (was what should be learned actually learned),
  precision (was anything fabricated), granularity (are facts too coarse or too
  fragmented).
- Emit a score table, write `eval/last-run.json`, and compare per-case deltas against
  `eval/baseline.json`. Freeze the baseline: `cp eval/last-run.json eval/baseline.json`
  and commit.
- `make eval-list` spends no tokens; it only lists the cases that would be evaluated.

This layer does not block CI, but **it must be run once before and after changing
`BuildPrompt`**. The corpus currently has only one synthetic `example-*` case (verifying
the harness wiring only); once real corpus is in place at M1, replace it with real
sessions.

## CI orchestration

| Job | Trigger | Duration target |
| --- | --- | --- |
| L0 + L1 + L2 + L3 | every push / PR | < 60s |
| L4 scenario matrix | every push / PR | < 90s |
| fuzz (5 minutes per target) | nightly (`.github/workflows/fuzz.yml`) | — |
| L5 eval | manual / label-triggered on prompt-change PRs | — |

> **fuzz ops note**: Go's fuzz engine **inline-minimizes** every "new interesting"
> input, and for large inputs (e.g. a 10KB body) under the default (unbounded)
> minimization budget the `execs` counter can sit at `0/sec` for tens of seconds — **it
> looks hung, but the engine is actually shrinking the sample**, not a bug in the code
> under test. Nightly caps this with `-fuzzminimizetime 30s`, and local reproduction is
> the same. All four targets pass, with no real crashers.

## Landing order

1. L2 hook contract + privacy (directly catches the known violation)
2. L3 CLI black-box skeleton (biggest payoff: 0% → a large chunk)
3. L2 determinism + supersession property tests
4. L4 scenario 4 (fusion) — verifies the architecture bet ✅
5. fuzz targets + nightly ✅
6. L5 eval skeleton ✅ (harness ready, awaiting M1 real corpus to replace the synthetic
   example)
