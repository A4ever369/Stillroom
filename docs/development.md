# Development guide

## Build & test

```bash
go build ./...        # everything
make still            # CLI → bin/still
go test ./...         # unit tests (CI also enforces gofmt + go vet)
bash scripts/smoke.sh # end-to-end without spending tokens (fake claude)
```

## How the pieces connect

```
SessionEnd hook ─→ internal/queue ─┐
                                   ├─→ cmd/still distill
claudecode.Discover (at-rest) ─────┘        │
        (ledger filters re-runs)            ▼
                              claudecode.DigestSession   transcript → prompt-sized digest
                                            ▼
                              redact.Text (input pass)
                                            ▼
                              distill.BuildPrompt + ClaudeRunner (`claude -p`)
                                            ▼
                              parseProposal → redact (output pass) → ir validation
                                            ▼
                              distill.Apply → .team-context/facts|playbooks
                                            ▼
                              materialize.Run → materialized.md ← CLAUDE.md imports
```

## Iterating on distillation quality (the core loop)

The prompt lives in one place: `BuildPrompt` in `internal/distill/distill.go`.
Iterate against real transcripts:

```bash
./bin/still distill --transcript ~/.claude/projects/<enc>/<id>.jsonl --dry-run
```

Judge output against three failure modes, in order of harm:

1. **Secret leakage** — must be zero; if a secret shape survives, add a rule
   to `internal/redact` *with a test* before touching the prompt.
2. **Generic noise** — facts that restate the repo or narrate steps; tighten
   the "NOT" clauses in the prompt.
3. **Missed durable knowledge** — the session clearly learned something and
   the proposal is empty; usually means the digest clipped the wrong part
   (see `maxDigestBytes` head/tail policy in the adapter).

## Adding a tool adapter (Codex, Cursor…)

1. Create `internal/adapter/<tool>/` with two entry points mirroring
   claudecode: `Discover(home, cwd) []Session` and
   `DigestSession(path) (Digest, error)` producing the same `Digest` shape
   (embed/alias claudecode's for now; extract a shared type when the second
   adapter lands).
2. Parsing must be tolerant: skip malformed lines, never fail the file.
3. Wire discovery into `pendingSessions` in `cmd/still/main.go`.
4. Fixture-based tests only — never commit real transcripts (they are
   evidence-plane data and may hold secrets even after redaction).

## Conventions

- Stdlib only (zero-dependency rule; see CLAUDE.md for all hard rules).
- Every behavior change lands with a test; bug fixes land with a regression
  test that fails before the fix.
- Update `docs/progress.md` (dated entry) for milestones and
  direction-level decisions.
- Knowledge/design vocabulary (facts, playbooks, supersession, two planes)
  is defined in `docs/design-v2.md` — reuse it, don't coin synonyms.
