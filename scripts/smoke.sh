#!/usr/bin/env bash
# L4 end-to-end scenario matrix (docs/testing.md L4), all with a FAKE `claude`
# — no tokens spent. Each scenario runs in its own isolated world (temp repo +
# CLAUDE_CONFIG_DIR/CODEX_HOME + fake claude on PATH) so a failure is localized
# and scenarios cannot leak state into one another.
#
# Scenarios:
#   1 cold-start   init → doctor → auto-discovery → distill (redaction) → materialize
#   2 hook-enqueue plugin SessionEnd → queue file → distill drains it → queue empty
#   3 idempotent   ledger dedupes a re-run; --force re-distills
#   4 codex        a Codex rollout for this repo is discovered and distilled
#   5 upgrade      an old .team-context layout gains new files on init, loses no data
#   6 review       still review renders a semantic knowledge diff
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

STILL="$WORK/still"
go build -o "$STILL" "$ROOT/cmd/still" || { echo "build failed"; exit 1; }

# A fake `claude` that ignores stdin and returns one fact carrying a secret, so
# every distill scenario also exercises redaction.
mkdir -p "$WORK/bin"
cat > "$WORK/bin/claude" <<'FAKE'
#!/usr/bin/env bash
cat > /dev/null
cat <<'JSON'
{"result": "{\"facts\":[{\"id\":\"deploy.acme.db-endpoint\",\"confidence\":\"high\",\"body\":\"Acme prod DB entry is pgbouncer on 6432; direct 5432 is blocked. token AKIAIOSFODNN7EXAMPLE\"}],\"playbook\":null}"}
JSON
FAKE
chmod +x "$WORK/bin/claude"
export PATH="$WORK/bin:$PATH"

pass=0
fail=0
assert_file()   { [ -f "$1" ] || { echo "  FAIL: expected file $1"; exit 1; }; }
assert_absent() { [ -e "$1" ] && { echo "  FAIL: expected $1 to be gone"; exit 1; }; return 0; }
assert_in()     { grep -q "$2" "$1" || { echo "  FAIL: '$2' not found in $1"; exit 1; }; }
assert_out()    { printf '%s' "$1" | grep -q "$2" || { echo "  FAIL: '$2' not in output"; exit 1; }; }
refute_tree()   { if grep -Rq "$1" "$2"; then echo "  FAIL: '$1' leaked into $2"; exit 1; fi; return 0; }

# claude_session writes a >=minTurns transcript into a repo's discovery dir.
claude_session() { # $1=claude_home $2=repo $3=name
  local enc; enc="$(printf '%s' "$2" | sed 's/[^a-zA-Z0-9]/-/g')"
  mkdir -p "$1/projects/$enc"
  cat > "$1/projects/$enc/$3.jsonl" <<'EOF'
{"type":"user","sessionId":"smoke-1","timestamp":"2026-07-21T10:00:00Z","message":{"content":"deploy the acme env"}}
{"type":"assistant","timestamp":"2026-07-21T10:01:00Z","message":{"content":[{"type":"text","text":"checking the db entry"},{"type":"tool_use","name":"Bash","input":{"command":"psql -p 5432"}}]}}
{"type":"user","timestamp":"2026-07-21T10:01:30Z","message":{"content":[{"type":"tool_result","content":"timeout: blocked"}]}}
{"type":"assistant","timestamp":"2026-07-21T10:03:00Z","message":{"content":[{"type":"text","text":"use pgbouncer 6432"}]}}
{"type":"user","timestamp":"2026-07-21T10:05:00Z","message":{"content":"ok ship it"}}
{"type":"assistant","timestamp":"2026-07-21T10:05:30Z","message":{"content":[{"type":"text","text":"done"}]}}
EOF
}

run() { # $1=name  $2=function
  echo "== $1 =="
  if ( set -e; "$2" ); then echo "  PASS"; pass=$((pass+1)); else fail=$((fail+1)); fi
}

# ---------------------------------------------------------------------------

scenario_cold_start() {
  local repo="$WORK/s1/repo"; mkdir -p "$repo"; git -C "$repo" init -q
  export CLAUDE_CONFIG_DIR="$WORK/s1/home"; unset CODEX_HOME || true
  claude_session "$CLAUDE_CONFIG_DIR" "$repo" sess
  cd "$repo"
  "$STILL" init >/dev/null
  "$STILL" doctor >/dev/null || { echo "  FAIL: doctor exit non-zero"; exit 1; }
  local out; out="$("$STILL" distill)"
  assert_out "$out" "redacted"
  assert_file .team-context/facts/deploy.acme.db-endpoint.md
  refute_tree "AKIAIOSFODNN7EXAMPLE" .team-context/
  assert_in .team-context/materialized.md "deploy.acme.db-endpoint"
  assert_in CLAUDE.md "@.team-context/materialized.md"
}

scenario_hook_enqueue() {
  local repo="$WORK/s2/repo"; mkdir -p "$repo"; git -C "$repo" init -q
  export CLAUDE_CONFIG_DIR="$WORK/s2/emptyhome"; unset CODEX_HOME || true # discovery finds nothing
  cd "$repo"
  "$STILL" init >/dev/null
  # A transcript OUTSIDE the discovery dir: the ONLY path to it is the queue.
  local t="$WORK/s2/external-session.jsonl"
  claude_session "$WORK/s2/tmphome" "$repo" x
  cp "$WORK/s2/tmphome/projects/"*/x.jsonl "$t"
  # Simulate the plugin's SessionEnd hook call.
  printf '{"transcript_path":"%s","cwd":"%s"}' "$t" "$repo" | "$STILL" hook session-end
  ls .team-context/queue/*.path >/dev/null 2>&1 || { echo "  FAIL: hook did not enqueue"; exit 1; }
  assert_out "$("$STILL" status)" "pending sessions: 1"
  "$STILL" distill >/dev/null
  assert_file .team-context/facts/deploy.acme.db-endpoint.md
  if ls .team-context/queue/*.path >/dev/null 2>&1; then echo "  FAIL: queue not drained"; exit 1; fi
  assert_out "$("$STILL" status)" "pending sessions: 0"
}

scenario_idempotent_and_force() {
  local repo="$WORK/s3/repo"; mkdir -p "$repo"; git -C "$repo" init -q
  export CLAUDE_CONFIG_DIR="$WORK/s3/home"; unset CODEX_HOME || true
  claude_session "$CLAUDE_CONFIG_DIR" "$repo" sess
  cd "$repo"
  "$STILL" init >/dev/null
  "$STILL" distill >/dev/null
  assert_out "$("$STILL" distill)" "nothing to distill"        # ledger dedupes
  assert_out "$("$STILL" distill --force)" "distilling"        # force re-runs
}

scenario_codex_discovery() {
  local repo="$WORK/s4/repo"; mkdir -p "$repo"; git -C "$repo" init -q
  export CLAUDE_CONFIG_DIR="$WORK/s4/emptyhome"           # no Claude Code sessions
  export CODEX_HOME="$WORK/s4/codex"
  local day="$CODEX_HOME/sessions/2026/07/21"; mkdir -p "$day"
  # A rollout whose session_meta.cwd matches this repo.
  { printf '{"timestamp":"2026-07-21T10:00:00.000Z","type":"session_meta","payload":{"session_id":"cx-1","cwd":"%s"}}\n' "$repo"
    cat <<'EOF'
{"timestamp":"2026-07-21T10:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"why is CI red"}]}}
{"timestamp":"2026-07-21T10:00:02.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"postgres image lacks vector ext"}]}}
{"timestamp":"2026-07-21T10:00:03.000Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":\"grep image ci.yml\"}"}}
{"timestamp":"2026-07-21T10:00:04.000Z","type":"response_item","payload":{"type":"function_call_output","output":"image: postgres:16"}}
{"timestamp":"2026-07-21T10:00:05.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"switch to pgvector image"}]}}
EOF
  } > "$day/rollout-2026-07-21T10-00-00-cx1.jsonl"
  cd "$repo"
  "$STILL" init >/dev/null
  assert_out "$("$STILL" doctor)" "1 Codex"
  "$STILL" distill >/dev/null
  assert_file .team-context/facts/deploy.acme.db-endpoint.md   # fake claude's fact, proving the codex session reached distill
}

scenario_upgrade() {
  # Hand-build an OLD-style .team-context: a fact already present, a .gitignore
  # missing the newer rule, no .gitattributes, no .gitkeep files.
  local repo="$WORK/s5/repo"; mkdir -p "$repo/.team-context/facts"; git -C "$repo" init -q
  cat > "$repo/.team-context/facts/legacy.fact.md" <<'EOF'
---
id: legacy.fact
scope: repo:old
observed_at: 2026-01-01T00:00:00Z
source: claude-code://old
confidence: high
status: active
---
A fact written by an older Stillroom version.
EOF
  printf 'queue/\n' > "$repo/.team-context/.gitignore"   # old layout: no .local/
  cd "$repo"
  "$STILL" init >/dev/null                                # in-place upgrade
  assert_in .team-context/.gitattributes "materialized.md merge=union"
  assert_in .team-context/.gitignore ".local/"
  assert_file .team-context/facts/.gitkeep
  assert_file .team-context/playbooks/.gitkeep
  assert_in .team-context/facts/legacy.fact.md "older Stillroom version"  # data preserved
}

fact_file() { # $1=path $2=id $3=body
  cat > "$1" <<EOF
---
id: $2
scope: repo:x
observed_at: 2026-07-21T00:00:00Z
source: claude-code://s
confidence: high
status: active
---
$3
EOF
}

scenario_review() {
  local base="$WORK/s6/base/.team-context/facts"
  local head="$WORK/s6/head/.team-context/facts"
  mkdir -p "$base" "$head"
  fact_file "$base/keep.me.md"   keep.me   "unchanged"
  fact_file "$head/keep.me.md"   keep.me   "unchanged"
  fact_file "$head/brand.new.md" brand.new "fresh knowledge"
  local out; out="$("$STILL" review --base "$WORK/s6/base" --head "$WORK/s6/head")"
  assert_out "$out" "stillroom-knowledge-diff"
  assert_out "$out" "brand.new"
  if printf '%s' "$out" | grep -q "keep.me"; then echo "  FAIL: unchanged fact shown"; exit 1; fi
}

run "cold-start"        scenario_cold_start
run "hook-enqueue"      scenario_hook_enqueue
run "idempotent+force"  scenario_idempotent_and_force
run "codex-discovery"   scenario_codex_discovery
run "upgrade-in-place"  scenario_upgrade
run "review-diff"       scenario_review

echo "-----------------------------"
echo "scenarios: $pass passed, $fail failed"
[ "$fail" -eq 0 ] || exit 1
echo "SMOKE OK"
