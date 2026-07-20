#!/usr/bin/env bash
# End-to-end smoke test with a FAKE `claude` binary — no tokens spent.
# Covers: init → doctor → hook enqueue → auto-discovery → distill (redaction,
# ledger) → idempotent re-run → materialize output.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> build"
go build -o "$WORK/still" "$ROOT/cmd/still"

echo "==> fake claude"
mkdir -p "$WORK/bin"
cat > "$WORK/bin/claude" <<'FAKE'
#!/usr/bin/env bash
cat > /dev/null
cat <<'JSON'
{"result": "{\"facts\":[{\"id\":\"deploy.acme.db-endpoint\",\"confidence\":\"high\",\"body\":\"Acme prod DB entry is pgbouncer on 6432; direct 5432 is blocked.\"}],\"playbook\":{\"id\":\"customer-onboarding-deploy\",\"title\":\"Customer onboarding deploy\",\"body\":\"## Steps\\n1. make deploy-prod\"}}"}
JSON
FAKE
chmod +x "$WORK/bin/claude"
export PATH="$WORK/bin:$PATH"

echo "==> demo repo + fake claude home"
REPO="$WORK/repo"
mkdir -p "$REPO" && git -C "$REPO" init -q
export CLAUDE_CONFIG_DIR="$WORK/claude-home"
ENC="$(printf '%s' "$REPO" | sed 's/[^a-zA-Z0-9]/-/g')"
mkdir -p "$CLAUDE_CONFIG_DIR/projects/$ENC"
cat > "$CLAUDE_CONFIG_DIR/projects/$ENC/sess-1.jsonl" <<'EOF'
{"type":"user","sessionId":"smoke-1","cwd":"/r","gitBranch":"main","message":{"content":"deploy the acme env"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"checking the db entry"},{"type":"tool_use","name":"Bash","input":{"command":"psql -p 5432"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"timeout: blocked"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"use pgbouncer 6432. password is DB_PASSWORD = \"hunter2hunter2hunter2\""}]}}
{"type":"user","message":{"content":"ok ship it"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}
EOF

cd "$REPO"
echo "==> init + doctor"
"$WORK/still" init
"$WORK/still" doctor

echo "==> distill (auto-discovery)"
"$WORK/still" distill | tee "$WORK/out1"
grep -q "redacted" "$WORK/out1" || { echo "FAIL: expected redaction"; exit 1; }
test -f .team-context/facts/deploy.acme.db-endpoint.md || { echo "FAIL: fact not written"; exit 1; }
! grep -R "hunter2" .team-context/ || { echo "FAIL: secret leaked into knowledge base"; exit 1; }

echo "==> idempotent re-run (ledger)"
"$WORK/still" distill | grep -q "nothing to distill" || { echo "FAIL: ledger did not dedupe"; exit 1; }

echo "==> materialized content"
grep -q "deploy.acme.db-endpoint" .team-context/materialized.md || { echo "FAIL: materialize"; exit 1; }
grep -q "@.team-context/materialized.md" CLAUDE.md || { echo "FAIL: CLAUDE.md import"; exit 1; }

echo "SMOKE OK"
