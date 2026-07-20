package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestClipKeepsHeadAndTailOnRuneBoundaries(t *testing.T) {
	s := strings.Repeat("中文内容一二三四五", 1000) // 15KB of multi-byte text
	out := clip(s, 1000)
	if len(out) > 1100 {
		t.Fatalf("clip did not shrink: %d bytes", len(out))
	}
	if !strings.Contains(out, "[...elided...]") {
		t.Fatal("elision marker missing")
	}
	if !strings.HasPrefix(out, "中文") || !strings.HasSuffix(out, "五") {
		t.Fatalf("head/tail not preserved: %q ... %q", out[:12], out[len(out)-12:])
	}
	for _, r := range out {
		if r == '�' {
			t.Fatal("clip split a multi-byte rune")
		}
	}
}
