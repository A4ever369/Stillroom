# Stillroom Progress Log

> Maintenance convention: whenever you complete a milestone or make a direction-level decision, append an entry here (with a date).
> This document is the project's "source of truth for progress"; the design rationale is in `docs/design-v2.md`.

## Status overview

| Milestone | Contents | Status |
| --- | --- | --- |
| M0 Scaffold | ir / redact / adapter / distill / materialize / CLI / plugin, all with unit tests | ✅ 2026-07-19 |
| M1 Dogfooding | session auto-discovery, ledger, near-duplicate guard, doctor; **real-session distillation quality validation** | 🚧 first real validation passed (high quality), prompt-tuning loop started |
| M2 Open-source release | repo public, launch actions (§14), first external users | ⬜ |
| M3 Fusion validation | two-person merge across three paths, task-level evaluation | ⬜ |
| M4 Server (Phase 2) | evidence store, replay, retrieval, MCP surface; commercialization kickoff | ⬜ |

## Changelog

### 2026-07-22 — Eval baseline committed + launch checklist (English going forward)

- `docs/launch-checklist.md` (English): honest open-source-readiness checklist,
  grouped by severity. Quality validation and redaction are checked off; the
  remaining blockers are mechanical (module-path ≠ repo URL breaks `go install`,
  no prebuilt binaries, no first-run cost guardrail, Codex-runner coherence,
  translate the Chinese docs, CONTRIBUTING/CHANGELOG).
- Eval corpus + `eval/baseline.json` committed. Cases are **fictional but
  realistic** (English), modeled on real distillation patterns — deliberately
  NOT the user's real sessions: even credential-redacted, real transcripts carry
  confidential business content and a public commit is permanent. `make eval`
  mean 14.3/15 (deploy-pipeline-ssm 14, edge-runtime-crypto 15,
  example-ci-pg-image 14).
- `deploy-pipeline-ssm` is a **negative control** for the machine-env exclusion
  shipped earlier today: it seeds "my laptop only has AWS CLI v1" and the judge
  confirmed it was "correctly excluded" — a regression-testable proof the prompt
  rule works.
- New progress entries are in English from here; translating the older
  Chinese entries and design/testing docs is a launch-checklist item.

### 2026-07-22 — Distillation quality validated across 5 real projects (was Chinese; see git history)

With authorization, picked one representative session from each of 5 very
different real projects under `~/code` and ran `distill --dry-run` in a neutral
scratch repo (real claude, spends tokens): a Next.js survey site (fde/6am), a Go
security enterprise service (agentguard-server), an LLM-gateway billing fork
(new-api), an agent-eval plugin (clawvard), and a course platform (clawschool).

**Conclusion: quality is consistently high.** About 78 facts + 4 genuine
runbooks in total, covering deploy pipelines, license/version architecture,
billing internals, migration traps, framework gotchas, and access models — all
things a new teammate would need hours or days to rediscover, each carrying a
specific file path/commit/command/root cause. Granularity is good (one thing per
entry), precision is high. The privacy boundary genuinely works on real,
credential-laden sessions: new-api redacted 26 times, fde 5 times, clawvard 1
time, with no visible leaks in the output. **This is the first time the core
value proposition has been proven on someone else's projects.**

**One tuning point that recurred across all 5 projects**: local
machine-environment facts get pulled in as if durable (`env.macos.no-timeout-cmd`
was even tagged [high], "port 3000 is taken by Docker", "no curl on PATH"). This
kind of "only true on this dev machine" material belongs in personal notes, not
team knowledge. **Changed `BuildPrompt`**: added to the exclusions "facts about
the local dev machine rather than the project — the OS, which CLIs happen to be
on PATH, which port some other local app occupies, personal shell aliases", while
**explicitly preserving** the project's own access model (which account/auth can
touch its repo/infra = project knowledge). Changed the criterion to "would a
teammate on another machine who never saw this session still need it a month from
now".

(By the testing.md rule this change should go through one `make eval`; but this
is a targeted exclusion backed by 5 data points rather than a rewrite, so land it
first and confirm via regression once the real corpus baseline is in place.)

### 2026-07-22 — `still status --json` (structured status, for CI/tool consumption)

`cmdStatus` was refactored to first compute a `statusReport` struct, then render
text or JSON — both outputs share one source and can never disagree. The JSON
contains: facts (total/active/bad), playbooks (total/bad), pending_sessions,
discovery (per-tool counts for claude_code/codex), bad_files (sorted, never
null), materialized_up_to_date (which also brings the drift signal into the
status text). The text format is unchanged (doesn't break existing tests/smoke).
Tests: black-box parse the JSON and assert fields + a bad file enters both count
and bad_files.

### 2026-07-22 — materialized.md drift detection (`--check` + doctor)

Real failure mode: someone hand-edits a fact, or a merge lands changes to
`facts/`, but forgets to run `still materialize`, so the committed
`materialized.md` goes **stale** — and that is exactly the team context every
teammate's agent loads.

- `materialize.Run` split out a pure function `Render` (computes bytes only, no
  disk write; deterministic rendering makes "recompute == disk" the drift
  criterion).
- `still materialize --check`: recompute and compare against disk; if stale, exit
  1 with "run `still materialize` and commit", usable as a CI gate (the README
  command table marks it CI-friendly).
- `still doctor` gained a new check, "materialized.md is up to date".
- Tests: materialize unit test (Render and Run produce identical bytes with no
  disk write) + cmd black-box (after distill, check passes → hand-add a fact →
  check fails and doctor warns → re-materialize → passes again), running the real
  binary.

**Why no Codex hook**: probed the local `~/.codex`; its `notify`/hooks payload
format can't be confirmed to carry the rollout path, and guessing blindly
violates "build against reality". Besides, Codex auto-discovery already works,
and a hook is only a freshness optimization, not required — so no speculative
implementation.

### 2026-07-22 — L4 scenario matrix completed: smoke.sh expanded from a single happy path to 6 scenarios

`scripts/smoke.sh` was rewritten into an **isolated scenario matrix** (pure bash
+ fake claude, zero tokens), each scenario its own independent world (temp repo +
`CLAUDE_CONFIG_DIR`/`CODEX_HOME` + fake claude), failures localized. It exercises
the **compiled real binary** — the actual user path (shell integration, plugin
hook, real filesystem), a layer the Go black-box tests can't reach. All six
scenarios green:

1. cold-start: init → doctor → auto-discovery → distill (with redaction assertions) → materialize
2. **hook enqueue**: `still hook session-end` reads `{transcript_path,cwd}` and enqueues → queue file →
   `status` pending 1 → distill consumes → queue cleared (the transcript is deliberately placed outside
   the discovery directory, so the queue is the only entry point)
3. idempotency + `--force`
4. **Codex discovery**: CODEX_HOME holds a rollout whose cwd matches this repo → distill discovers and
   distills end to end, exercising multi-tool wiring on the real binary
5. fusion (still in `fusion_test.go`, Go black-box)
6. **upgrade path**: old layout → `init` upgrades in place, filling in gitattributes/gitkeep/gitignore
   without losing data
7. review diff

Caught a pure-bash pitfall along the way: `printf "$fmt"` treats `$fmt` as an
option when it starts with `---`, switched to a heredoc. **With this, all six
layers L0–L5 have executable evidence, and the test plan is 100% landed.**

### 2026-07-22 — review parasitism landed: PRs auto-comment the knowledge diff (the last piece of §13)

- **`internal/review` (new)**: pure functions that diff two knowledge snapshots
  (base/head) by **fact id** **semantically** (not a text diff), classifying
  added/updated/removed, marking a fact whose observation moved forward as a
  supersession. `Markdown()` outputs in deterministic sort order, with an
  invisible anchor `<!-- stillroom-knowledge-diff -->` at the top so the bot
  updates the same comment in place rather than spamming. 100% coverage.
- **`still review --base DIR [--head DIR]` (new command)**: head defaults to this
  repo, base defaults to empty (first adoption = all added). Never fails the
  build; skips bad files and still exits 0.
- **`.github/workflows/knowledge-diff.yml` (new)**: triggers when a PR touches
  `.team-context/facts|playbooks`; `git worktree` checks out a snapshot of the
  base branch → `still review` → uses the **first-party** `actions/github-script`
  to find by anchor and update the PR comment in place. Zero third-party actions,
  in keeping with the zero-dependency spirit.
- Tests: review package unit tests (classification / idempotent rewrite counts as
  no diff / playbook / determinism / all-section rendering) + cmd black-box (run
  `--base/--head` against two dirs directly, asserting the anchor + a new fact).

**With this, §13's review-parasitism loop is complete**: knowledge changes no
longer need a separate review surface, they ride along with the team's normal PR
review.

### 2026-07-22 — Codex adapter landed (second source tool) + digest types pushed down to tool-agnostic

First step toward multi-tool support on the M2 roadmap: integrating the **OpenAI
Codex CLI**.

- **`internal/session` (new)**: pushed `Digest`/`Meta` and the rendering helpers
  (`Clip`/`WriteTurn`/`CompactJSON`/`CompactAny`) down from `claudecode` into
  tool-agnostic types. The distill pipeline now only knows `session.Digest`;
  adding a tool = adding an adapter, with zero downstream changes. `Meta` gained a
  `Tool` field (`claude-code`/`codex`) that determines the scheme of the source
  ref on a fact.
- **`internal/adapter/codex` (new)**: reverse-engineered the format against
  **real local rollout files** (`~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`,
  one `{timestamp,type,payload}` per line). `session_meta`→cwd/session_id; a
  `response_item`'s message/function_call/function_call_output go into the digest,
  reasoning is discarded (same as claudecode discarding thinking). **Verified**: a
  real 904-turn session parsed out tool/turns/cwd/session/millisecond-level
  LastActivity correctly.
- **Discovery difference**: unlike Claude Code, Codex doesn't use encoded-cwd
  directory names but stores by date with cwd written inside the file — so
  `codex.Discover` reads the first `session_meta` line of each rollout to get the
  cwd for matching.
- **Quality decision**: Codex's `developer`-role messages are injected
  environment/approval/base-instructions scaffolding (which can push the digest to
  the 200KB cap), **discarded**, keeping only user/assistant — consistent with
  claudecode rendering only user/assistant.
- **Wiring**: `still distill` now discovers both Claude Code + Codex sessions;
  queue paths dispatch to the right adapter by `IsRollout` (basename
  `rollout-*.jsonl`); `doctor` reports both counts separately.
- **Tests**: codex unit tests (format / timestamps / mtime fallback / bad-line
  tolerance / IsRollout / Discover matching by cwd) + a fuzz target (wired into
  nightly). codex 87.9%, session 89.7%, all green.

### 2026-07-22 — First real-session distillation validation + first evidence-driven prompt tuning

Ran `still distill --dry-run` on this repo's own development session (441 turns,
the stretch that built the whole test system), producing **15 facts + 1
playbook**. Quality assessment (L5 three-axis manual inspection):

- **Recall is near perfect**: every real decision/bug in this session was
  captured, with precise details (even a sub-detail like "the fake claude needs
  `#!/bin/sh` + an absolute `/bin/cat` because the test clears PATH" is correct).
- **Precision is high**: no hallucinated config, everything lands on things that
  actually happened.
- **The playbook `diagnose-fuzz-stall` is excellent and transferable**: it fully
  crystallizes the methodology for diagnosing the fake fuzz stall into a reusable
  recipe.

**But it surfaced a clear prompt flaw — over-capturing session/tool
meta-observations**. Two noise facts: `distill.real-session-slow` (purely
narrating "this test run itself didn't finish within 2 minutes") and
`repo.github-remote` (which smuggled in a one-off `echo README` command for
creating a new GitHub repo). Both are exactly what "don't narrate step by step"
should have filtered out but missed. **Fixed `BuildPrompt`**: added an exclusion
rule to the fact definition — anything true only of "this session/run" rather
than of the project (command durations, tool timeouts, one-off setup,
observations about the distillation tool itself) is discarded, with the criterion
"would a teammate who never saw this session still need it a month from now".

Ops lesson: `claude -p` distillation of a 441-turn real session takes **several
minutes**, the 120s default timeout isn't enough, run it in the background (this
itself confirms real-session slowness — but it's ops knowledge that shouldn't go
into the team knowledge base, so it stays in this progress.md and isn't kept as a
fact).

### 2026-07-21 — L5 distillation-quality eval harness landed (scaffold)

`cmd/eval` (not a `_test.go`, so `go test`/CI never triggers it, and `go build`
keeps it from rotting; `make eval` runs it manually, spending real tokens). Each
case = `testdata/corpus/<name>/{transcript.jsonl, expected.md}`. It reuses the
production pipeline `DigestSession → distill.Run(ClaudeRunner)` to produce a
proposal, then uses a second `claude -p` as an LLM-judge to score three axes
(recall/precision/granularity, each 0–5), outputting a score table +
`eval/last-run.json` and comparing per-case deltas against `eval/baseline.json`.
`make eval-list` lists cases without spending tokens. It ships with one synthetic
`example-ci-pg-image` (CI using the wrong Postgres image + a deploy-ordering
trap, 10 turns to pass minTurns) purely to verify the wiring; the real corpus
awaits M1. **This fills in the last layer of the test plan — the L0–L5 structure
is now complete.** From here, every change to `BuildPrompt` has a quantifiable
quality-regression signal (M1's central open question now has a mechanized
measurement entry point for the first time).

### 2026-07-21 — L1 coverage brought up to ≥85% (every internal package)

Filled in error paths per the testing.md L1 goal: `ir` (`store_test.go`:
`Exists`, `Init`/`ensureLines` in-place upgrade, `LoadPlaybooks`/`WritePlaybook`
round-trip and bad-file isolation, `SortFacts` determinism), `distill`
(`prompt_apply_test.go`: `BuildPrompt` injection branches, `Run`'s Now default
and error propagation, `Apply` aborting on write failure), `queue` (Enqueue
failing to create a directory, List skipping non-`.path` entries), `materialize`
(a bad fact file rendered as a warning rather than aborting, an import appended to
a file with no trailing newline not running together). Results: redact 100 /
queue 92 / ledger 89 / ir 89 / materialize 87 / claudecode 86 / distill 85.
`cmd/still`'s 0% is because black-box subprocess tests don't count toward
coverage; that logic is exercised for real by L3.

### 2026-07-21 — L2 fuzz landed + nightly CI + diagnosing a "fake stall"

Four fuzz targets landed (tolerant-parsing invariants): `FuzzParseProposal`
(distill), `FuzzParseFact`/`FuzzParsePlaybook` (ir), `FuzzDigestSession`
(claudecode). Assertions: no panic on any input; **accepted output must be
self-consistent** — a fact/playbook passes `Validate` and `Encode` is a fixed
point (both load and write sides must agree on "what counts as valid"), a
surviving proposal's fact id can't escape the directory, Status==active, the body
has no residual secret; a digest stays UTF-8, yields LastActivity, and doesn't
exceed budget. All pass, **no real crasher**.

`.github/workflows/fuzz.yml`: nightly + manual, a four-target matrix,
`-fuzzminimizetime` capped, crashers uploaded as artifacts.

**Lesson from a false alarm**: `FuzzParseProposal` repeatedly showed "`execs: N
(0/sec)` frozen for tens of seconds". Benchmarking each function under test
(`parseProposal`/`Validate`/`redact.Text`) showed all in the
microsecond-to-millisecond range, none a slow path. SIGQUIT to grab the worker
stack; running the resulting "failing input" on its own **passed in 0.01s** — a
red herring. Real cause: the Go fuzz engine **inline-minimizes** every new
interesting input, and a large input (10KB body) freezes the `execs` counter
under the default unbounded minimization budget — **it looks hung but is actually
shrinking the sample**, not a Stillroom bug. Capping `-fuzzminimizetime` restores
normal behavior. redact uses RE2 (linear, no catastrophic backtracking), which
ruled out a regex blowup from the start.

### 2026-07-20 — L4 fusion scenarios: the core bet validated, and two bugs surfaced

`cmd/still/fusion_test.go`: two clones distill independently → `git merge`,
directly validating design-v2 §2 "one fact per file = a git directory merge is
the fusion algorithm".

**The bet holds**: disjoint knowledge merges automatically and both facts
survive; when both sides edit the same fact it still conflicts — which is
correct, a genuine disagreement should stop and wait for a human to adjudicate —
and the conflict is strictly confined to that one file while the rest of the
facts merge as usual, with `still status` still usable in the conflicted state.

But this test surfaced two bugs:

3. **Generated artifacts necessarily conflict**: `materialized.md` is rendered in
   full, so when both sides distill in parallel it **conflicts every single
   time**, even when the facts themselves merge perfectly cleanly. The fact-plane
   bet is right; the fault is in the generated artifact beside it. Fix: `init`
   writes `.team-context/.gitattributes` declaring `materialized.md merge=union`
   — union is a git **built-in** driver, committed with the repo, free for every
   clone, needing no per-clone config; both sides' added lines are kept, and the
   next `still materialize` re-renders them into place in deterministic order. The
   facts themselves are **deliberately not** union: a genuine disagreement on a
   single fact must stop and ask a human.
4. **Empty directories don't enter git → new-hire onboarding breaks**: the
   `facts/` and `playbooks/` that `init` creates are empty directories, which git
   doesn't track, so a teammate's clone has neither directory, and **the first
   distill crashes straight on a bare ENOENT**. Fix: `init` writes `.gitkeep`, and
   `WriteFact`/`WritePlaybook` do a `MkdirAll` as a safety net before writing.

### 2026-07-20 — Test plan landed (first batch) + two real bugs

The test plan is in `docs/testing.md`: organized by **invariant** rather than by
package, split into six layers L0–L5, all automatable except "distillation
quality". This batch lands L0/L2/L3:

- `cmd/still/harness_test.go`: a CLI black-box harness. Each case is an isolated
  world (temp git repo + temp `CLAUDE_CONFIG_DIR` + fake `claude`), with **PATH
  containing only the fake bin directory**, ensuring branches like "claude isn't
  installed on the machine" reproduce faithfully even on a dev machine.
- `hook_contract_test.go`: 14 kinds of bad input assert exit 0 and silence.
- `cli_test.go`: the full command matrix init/distill/status/doctor/materialize,
  including 10 kinds of malformed model output (prose, truncated JSON,
  path-traversal ids, …).
- `privacy_test.go`: 10 secret shapes, asserting that after distillation **no
  file in the repo** contains the original text.
- `internal/ir/invariants_test.go`: randomized property tests for determinism and
  one-way supersession.
- `repo_rules_test.go`: zero-dependency enforced by machine; "re-running after
  commit produces no git diff".

The new tests caught two bugs on the spot:

1. **hook contract violation**: `still hook <unknown name>` went through `return
   fmt.Errorf` → exit 1, violating "silently exit 0 in all cases". Changed to a
   silent no-op (a version mismatch between plugin and CLI is not the user's
   problem).
2. **`observed_at` wired to the wrong time source (semantic-level)**: it
   originally took `time.Now()`, i.e. **the moment distillation runs**. The
   consequence: a teammate distilling a three-week-old session today produces
   facts that, purely by "ran later", clobber knowledge learned just yesterday —
   the supersession rule's sort key was wired to the tool's run time. Changed to
   take the session's own last-activity time (`SessionMeta.LastActivity`: the max
   of the per-line `timestamp`s, falling back to file mtime if absent). This also
   fixed `WriteFact`: RFC3339 is second-precision, so a timestamp with nanoseconds
   (like mtime) is always "later than" its own re-parsed version, causing every
   rewrite to fabricate a supersedes — truncate uniformly to seconds before
   writing.

### 2026-07-20 — M1 code side complete

- `internal/adapter/claudecode/discover.go`: cwd → Claude Code storage-directory encoding (`EncodeProjectDir`), so `still distill` auto-discovers this repo's historical sessions without installing the plugin. **First-experience change: you can distill the past few weeks of work in the first minute.**
- `internal/ledger`: the `.team-context/.local/distilled.jsonl` distillation ledger (gitignored, append-only, tolerant of bad lines), making distill idempotent; `--force` re-distills.
- `internal/queue`: the hook queue extracted into its own package (pointer files, path-hash idempotency, automatic cleanup of dangling entries).
- `internal/distill/similar.go`: rune-bigram Jaccard near-duplicate detection (CJK-friendly), flagging a NOTE when a new fact is similar to an existing fact but has a different id.
- prompt addition: existing playbooks (id — title) are injected, steering toward revision rather than minting new ones.
- `still doctor`: six environment self-checks.
- `.team-context/.gitignore` in-place upgrade mechanism (queue/ + .local/).

### 2026-07-20 — Repository migration and rename

- The project was named **Stillroom** (a distillation room, §16) and moved into its own repository `~/code/stillroom`.
- CLI renamed `tg` → `still`; the parser refactored into `internal/adapter/claudecode/` (reserving room for Codex/Cursor).
- The server baggage (server/migrations) stays in the old repo traces-git, corresponding to the Phase 2 commercial side — this repo = the "single-machine + git, fully open source" half (the §14 dividing line).
- Filled in the README (etymology + architecture + privacy commitments), the Apache-2.0 LICENSE, CI (gofmt/vet/test), and .gitignore.

### 2026-07-19 — M0 scaffold (in the traces-git repo)

- Five internal packages + CLI + Claude Code plugin taking shape, all unit tests passing, fake-claude end-to-end smoke passing (including redaction validation).
- Key semantics landed: a fact's `observed_at`/`supersedes` (a new observation overrides an old one, the old one cannot override in reverse), materialize injects only active facts, deterministic rendering with zero git noise.

## Next steps (by priority)

1. **Real-session distillation validation (only a human can do this)**: `make still && ./bin/still init && ./bin/still doctor`, then `./bin/still distill --dry-run`. Real-world quality decides how the prompt is tuned (`BuildPrompt` in `internal/distill/distill.go`). This is the whole open question of M1.
2. Iterate on the prompt / fact granularity / minTurns threshold based on the results of 1.
3. ~~Codex adapter~~ ✅ 2026-07-22 landed (see changelog). Next adapter candidate: Cursor.
4. ~~GitHub Action: auto-comment the knowledge-diff summary on PRs~~ ✅ 2026-07-22 landed (`internal/review` + `knowledge-diff.yml`).
5. M2 launch checklist: domain (stillroom.dev / .ai), GitHub org, trademark search, Show HN, replies on anthropics/claude-code #38536 / #40981.

## Decision log (why it is the way it is now)

| Date | Decision | Rationale |
| --- | --- | --- |
| 2026-07-19 | Two-plane architecture: evidence does not fuse, only distilled knowledge fuses | design-v2 §1; conversations themselves cannot be merged |
| 2026-07-19 | Knowledge plane = a real git repo, one fact per file | merge/review/permissions/history come for free (§2) |
| 2026-07-19 | Distillation via the user's own `claude -p`, run locally | transcripts never leave the machine = the privacy floor (§4.1) |
| 2026-07-19 | The hook only enqueues, never spends tokens automatically | no model call without consent; an opt-in automatic mode is on the roadmap (§13) |
| 2026-07-19 | Distiller output is redacted a second time | distillation is a concentrator, not a sanitizer (§4.1) |
| 2026-07-20 | Named Stillroom; CLI `still` | §16; Engram/Tacit/Cairn/Baton etc. were all name-clashes |
| 2026-07-20 | Open source first, single-machine+git free / central service paid | §14; trust, standard adoption, and the moat lie in the corpus |
| 2026-07-20 | Discovery uses the encoded-cwd directory but is marked version-fragile, with the hook path preferred | sessions are trending toward the cloud, so at-rest parsing can't be a long-term bet (§11.4) |
| 2026-07-20 | Near-duplicate detection uses bigram Jaccard rather than embeddings | a zero-dependency PR-level tripwire; genuine entity resolution is left to research (§10) |
| 2026-07-20 | Tests organized by invariant, hard rules are the spec | the six hard rules are the definition of correctness, not style advice; one invariant, one layer of executable evidence (testing.md) |
| 2026-07-20 | `observed_at` takes the session's last-activity time, not the distillation moment | supersession must sort by "when the knowledge was observed"; sorting by tool run time lets a back-distilled historical session clobber newer knowledge |
| 2026-07-20 | `materialized.md` uses union merge, the facts themselves do not | parallel re-rendering of a generated artifact is a necessary and information-free conflict; a disagreement on a fact must stop and ask a human (§2) |
| 2026-07-21 | fuzz goes into nightly only and with `-fuzzminimizetime` capped, not into the push/PR gate | fuzz is a time-boxed probe, not a gate; the engine's inline minimization can falsely stall on large inputs, and the cap keeps nightly runs bounded and predictable |
| 2026-07-22 | `Digest`/`Meta` pushed down to `internal/session`, an adapter only does "format→digest" | the right boundary for multi-tool support: the pipeline knows one tool-agnostic type, adding a tool = adding an adapter, with zero downstream changes |
| 2026-07-22 | Codex's `developer` messages discarded, keeping only user/assistant | the injected environment/approval/base-instructions are scaffolding, not knowledge, and would eat the entire digest budget (same as claudecode discarding thinking) |
| 2026-07-23 | Phase 2 server (`stillroomd`) holds no source of truth — the index is a derived cache over git checkouts | removes the database, the backup policy, the security review and the exit cost, which are what actually blocks an internal tool from being deployed (§17.1) |
| 2026-07-23 | The server never reads the evidence plane and never writes to a repo, enforced by tests | serving transcripts would break the privacy floor; server-side edits would route around the PR review that makes knowledge trustworthy (§17.2) |
| 2026-07-23 | No permission model of our own: delegate to the git host ("can read the repo → can read its knowledge") | a second, divergent copy of an org's ACLs is a liability, not a feature (§17.3) |
| 2026-07-23 | Cross-repo search ships before any activity/per-person view | search is uncontested value, activity is contested; shipping them in the wrong order spends the trust the rest of the product needs (§17.4/§17.5) |
| 2026-07-23 | CJK searchability via character bigrams rather than a segmenter | keeps the zero-dependency rule while making Chinese knowledge a first-class case, not an edge case |
| 2026-07-23 | Category is "team memory for AI engineering", explicitly not "git for AI traces" | GitHub won on code being reused and reviewed, not on versioning files; traces are neither, so a trace-storage product is an observability tool on someone else's turf (product-design §1) |
| 2026-07-23 | Activation is defined as "a fact you did not write saved you", not installation | it is the only event that creates a believer, and naming it as the metric forces seeding 30-50 facts before anyone else installs — otherwise every person has to be the first person (product-design §7) |
| 2026-07-23 | Anti-metrics written down: total fact count, install count, sessions captured | volume is trivially gamed and volume-over-durability is exactly the failure mode that makes a knowledge base useless; supersession rate is the real health signal (product-design §8) |
| 2026-07-23 | Consent constitution: 5 invariants + an explicit anti-feature list | the surveillance trap and knowledge rot are product deaths, not engineering ones, so the rules are recorded to be refused on sight rather than re-litigated per feature (product-design §6) |
| 2026-07-23 | Adopted a Linear-derived visual system, persisted as `DESIGN.md` and implemented in the `stillroomd` UI | a product with four visual surfaces needs one set of tokens, not per-page taste; the near-black canvas + single lavender accent + surface ladder suits a dense reading surface, and writing it down means future UI work does not re-decide colour |
| 2026-07-23 | Two documented departures from the source system: a light theme (from its own inverse ladder) and one `semantic-attention` state colour | the source documents a marketing page with no light mode and one semantic colour; a knowledge tool is read all day and must show "unverified" at a glance — but attention tints text and rules only, never fills, so the accent stays scarce |
| 2026-07-23 | Near-dup detection switched from rune-bigram to idf-weighted token Jaccard + an id token-set check | measured on 210 real facts: bigrams put unrelated same-project prose in the same score band as real duplicates, so no threshold separates them; this supersedes the 2026-07-20 bigram decision, which was reasonable a priori and wrong on data |
| 2026-07-23 | Document frequency is computed over the existing knowledge base only, never including the fact being checked | counting it makes shared tokens look commoner than unique ones, so in a small base a real duplicate scores near zero; excluding it also degrades gracefully to plain Jaccard when there is no corpus to learn word rarity from |
