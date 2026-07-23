# Stillroom

CLI (`still`) that distills local AI coding sessions into a git-native team
knowledge base (`.team-context/`: facts + playbooks) and materializes it back
into agent context via CLAUDE.md imports.

## Commands

- Build: `go build ./...` — CLI binary: `make still` → `bin/still`
- Test: `go test ./...` (must stay green; CI also enforces `gofmt -l` clean and `go vet`)
- E2E smoke without spending tokens: `bash scripts/smoke.sh` (uses a fake `claude` binary)

## Map

| Path | Role |
| --- | --- |
| `cmd/still` | CLI: init / distill / materialize / status / doctor / hook |
| `cmd/stillroomd` | self-hostable server: org-wide search over many repos (§17) |
| `internal/ir` | knowledge model: Fact (observed_at/supersedes/status), Playbook, Store |
| `internal/session` | tool-agnostic `Digest`/`Meta` + shared render helpers (clip, turns) |
| `internal/adapter/claudecode` | Claude Code adapter: session discovery + transcript → digest |
| `internal/adapter/codex` | Codex CLI adapter: `rollout-*.jsonl` discovery + digest (same `session.Digest`) |
| `internal/distill` | prompt build, `claude -p` runner, proposal parse, near-dup check |
| `internal/materialize` | render materialized.md (active facts only, deterministic) |
| `internal/review` | semantic knowledge diff → PR-comment markdown (`still review`, §13) |
| `internal/index` | in-memory search index over many repos' knowledge planes (server-side, read-only) |
| `internal/redact` | secret scrubbing (runs on digest input AND distiller output) |
| `internal/queue` / `internal/ledger` | hook queue / distilled-session ledger (both machine-private) |
| `plugin/claude-code` | SessionEnd hook plugin (enqueue only — never calls a model) |

Design doc: `docs/design-v2.md` (two-plane architecture, fusion semantics, roadmap).
Progress ledger: `docs/progress.md` — **append an entry there when you complete
a milestone or make a direction-level decision.**

## Hard rules

- **Zero dependencies.** Stdlib only; the tiny frontmatter parser in
  `internal/ir` is deliberate — do not add a YAML library.
- **Privacy invariants:** transcripts never leave the machine; distiller
  output is redacted before it touches disk; the hook never spends tokens.
- **Determinism:** Fact/Playbook `Encode()` and materialize output must be
  byte-stable — repeated runs may not produce git diffs.
- **Tolerant parsing:** transcript formats drift across tool versions; skip
  malformed lines, never fail a whole file on one bad entry.
- **Hook contract:** `still hook ...` must exit 0 silently on any problem —
  it is not allowed to break a user's session.
- **Server plane (`stillroomd`) owns no source of truth.** It only indexes
  `.team-context/` from git checkouts: never reads the evidence plane
  (transcripts, `queue/`), never writes to a repo, and the CLI must keep
  working with no server at all (§17.2).
- Supersession only moves forward: a newer `observed_at` replaces, an older
  one never clobbers (see `Store.WriteFact`).
