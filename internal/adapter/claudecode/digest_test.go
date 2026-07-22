package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDigestClaudeSessionEnvelopeShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"user","sessionId":"abc-123","cwd":"/repo","gitBranch":"main","version":"2.1.0","message":{"content":"部署 acme 客户环境"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"好的,先看配置。"},{"type":"tool_use","name":"Bash","input":{"command":"make deploy"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"deploy ok"}]}}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"内心独白不该出现"},{"type":"text","text":"完成。"}]}}
not-json
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := DigestSession(path)
	if err != nil {
		t.Fatalf("DigestSession: %v", err)
	}
	if d.Meta.SessionID != "abc-123" || d.Meta.CWD != "/repo" || d.Meta.GitBranch != "main" {
		t.Fatalf("meta = %+v", d.Meta)
	}
	for _, want := range []string{"部署 acme", "tool:Bash", "make deploy", "deploy ok", "完成。"} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("digest missing %q:\n%s", want, d.Text)
		}
	}
	if strings.Contains(d.Text, "内心独白") {
		t.Error("thinking blocks must be dropped from the digest")
	}
	if d.Meta.Turns < 4 {
		t.Errorf("turns = %d, want >= 4", d.Meta.Turns)
	}
}

// observed_at must come from the session, not from the clock at distill time:
// distilling a three-week-old session later must not let it outrank knowledge
// learned since. See SessionMeta.LastActivity.
func TestDigestTakesLastActivityFromTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := strings.Join([]string{
		`{"type":"user","sessionId":"s1","timestamp":"2026-05-01T10:00:00Z","message":{"content":"start"}}`,
		`{"type":"assistant","timestamp":"2026-05-01T12:30:00Z","message":{"content":"middle"}}`,
		// Out of order on purpose: a resumed session can rewind.
		`{"type":"user","timestamp":"2026-05-01T11:00:00Z","message":{"content":"resumed"}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := DigestSession(path)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 5, 1, 12, 30, 0, 0, time.UTC)
	if !d.Meta.LastActivity.Equal(want) {
		t.Errorf("LastActivity = %s, want the latest timestamp %s", d.Meta.LastActivity, want)
	}
}

// Transcripts without per-line timestamps still need an observation time;
// the file's mtime is the documented fallback.
func TestDigestFallsBackToFileMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"content":"no timestamps here"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2026, 3, 2, 1, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	d, err := DigestSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Meta.LastActivity.Equal(mtime) {
		t.Errorf("LastActivity = %s, want the file mtime %s", d.Meta.LastActivity, mtime)
	}
}
