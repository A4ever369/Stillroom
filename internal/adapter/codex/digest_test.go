package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A synthetic rollout in the real Codex shape: {timestamp,type,payload} lines,
// session_meta for cwd/session, response_item messages/tool calls, plus the
// telemetry (event_msg/turn_context) and reasoning we must drop.
const sampleRollout = `{"timestamp":"2026-07-20T07:24:20.100Z","type":"session_meta","payload":{"session_id":"019f-abc","cwd":"/repo/acme","id":"019f-abc"}}
{"timestamp":"2026-07-20T07:24:21.000Z","type":"turn_context","payload":{"cwd":"/repo/acme","model":"gpt"}}
{"timestamp":"2026-07-20T07:24:22.000Z","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"<environment>framing that must be dropped</environment>"}]}}
{"timestamp":"2026-07-20T07:24:23.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"部署 acme 客户环境"}]}}
{"timestamp":"2026-07-20T07:24:24.000Z","type":"response_item","payload":{"type":"reasoning","summary":"internal monologue must not appear","encrypted_content":"xxxx"}}
{"timestamp":"2026-07-20T07:24:25.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"好的,先看配置。"}]}}
{"timestamp":"2026-07-20T07:24:26.000Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":\"make deploy\"}","call_id":"c1"}}
{"timestamp":"2026-07-20T07:24:27.500Z","type":"response_item","payload":{"type":"function_call_output","output":"deploy ok","call_id":"c1"}}
{"timestamp":"2026-07-20T07:24:28.000Z","type":"event_msg","payload":{"type":"token_count","turn_id":"t1"}}
not-json-tolerated
{"timestamp":"2026-07-20T07:24:29.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"完成。"}]}}
`

func writeRollout(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout-2026-07-20T07-24-20-019f-abc.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDigestCodexRolloutShape(t *testing.T) {
	d, err := DigestSession(writeRollout(t, sampleRollout))
	if err != nil {
		t.Fatalf("DigestSession: %v", err)
	}
	if d.Meta.Tool != "codex" {
		t.Errorf("tool = %q, want codex", d.Meta.Tool)
	}
	if d.Meta.SessionID != "019f-abc" || d.Meta.CWD != "/repo/acme" {
		t.Fatalf("meta = %+v", d.Meta)
	}
	for _, want := range []string{"部署 acme", "tool:shell", "make deploy", "deploy ok", "完成。"} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("digest missing %q:\n%s", want, d.Text)
		}
	}
	// Framing and reasoning must not leak into the digest.
	if strings.Contains(d.Text, "framing that must be dropped") {
		t.Error("developer/framing message must be dropped")
	}
	if strings.Contains(d.Text, "internal monologue") {
		t.Error("reasoning blocks must be dropped")
	}
	// user + assistant + tool:shell + result + assistant = 5 turns.
	if d.Meta.Turns != 5 {
		t.Errorf("turns = %d, want 5", d.Meta.Turns)
	}
}

func TestDigestCodexLastActivityIsLatestTimestamp(t *testing.T) {
	d, err := DigestSession(writeRollout(t, sampleRollout))
	if err != nil {
		t.Fatal(err)
	}
	// The last line's timestamp, in UTC, millisecond precision preserved.
	want := time.Date(2026, 7, 20, 7, 24, 29, 0, time.UTC)
	if !d.Meta.LastActivity.Equal(want) {
		t.Errorf("LastActivity = %s, want %s", d.Meta.LastActivity, want)
	}
}

func TestDigestCodexFallsBackToMtime(t *testing.T) {
	// A rollout with no parseable timestamps still needs an observation time.
	path := writeRollout(t, `{"type":"session_meta","payload":{"session_id":"s","cwd":"/r"}}`)
	mtime := time.Date(2026, 3, 2, 1, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	d, err := DigestSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Meta.LastActivity.Equal(mtime) {
		t.Errorf("LastActivity = %s, want file mtime %s", d.Meta.LastActivity, mtime)
	}
}

func TestDigestCodexKeepsGoodLinesAmongBadOnes(t *testing.T) {
	content := strings.Join([]string{
		`{"type":"session_meta","payload":{"session_id":"keepme","cwd":"/r"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":`, // truncated
		"\x00\x01 garbage",
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"survived"}]}}`,
	}, "\n")
	d, err := DigestSession(writeRollout(t, content))
	if err != nil {
		t.Fatalf("one bad line failed the whole file: %v", err)
	}
	if !strings.Contains(d.Text, "survived") {
		t.Errorf("lost a good line to a neighbouring bad one:\n%s", d.Text)
	}
	if d.Meta.SessionID != "keepme" {
		t.Errorf("lost session metadata to a bad line")
	}
}

func TestIsRollout(t *testing.T) {
	cases := map[string]bool{
		"/x/sessions/2026/07/20/rollout-2026-07-20T07-24-20-uuid.jsonl": true,
		"rollout-abc.jsonl":             true,
		"/x/projects/foo/session.jsonl": false, // Claude Code shape
		"rollout-abc.txt":               false,
		"notes.md":                      false,
	}
	for path, want := range cases {
		if got := IsRollout(path); got != want {
			t.Errorf("IsRollout(%q) = %v, want %v", path, got, want)
		}
	}
}
