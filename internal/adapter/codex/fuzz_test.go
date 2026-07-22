package codex

// Fuzzing Codex rollout digestion (docs/testing.md L2, "tolerant parsing").
// Same contract as the Claude Code adapter: a malformed line is skipped, never
// fatal to the whole file, and clipping never splits a rune.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/0xbeekeeper/stillroom/internal/session"
)

func FuzzDigestSession(f *testing.F) {
	f.Add(sampleRollout)
	f.Add("")
	f.Add("\n\n\n")
	f.Add("not json at all")
	f.Add(`{"type":"session_meta","payload":{}}`)
	f.Add(`{"type":"response_item","payload":{"type":"message","role":"user","content":"unterminated`)
	f.Add(`{"timestamp":"not-a-date","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"x"}]}}`)
	f.Add(`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + strings.Repeat("字", 5000) + `"}]}}`)
	f.Add(`{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":` + strings.Repeat("[", 200) + strings.Repeat("]", 200) + `}}`)

	f.Fuzz(func(t *testing.T, content string) {
		path := filepath.Join(t.TempDir(), "rollout-x.jsonl")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Skip()
		}
		d, err := DigestSession(path)
		if err != nil {
			t.Fatalf("digesting a readable file failed: %v\ninput: %q", err, content)
		}
		if len(d.Text) > session.MaxDigestBytes {
			t.Fatalf("digest exceeded its budget: %d > %d bytes", len(d.Text), session.MaxDigestBytes)
		}
		if utf8.ValidString(content) && !utf8.ValidString(d.Text) {
			t.Fatalf("clipping split a rune: valid UTF-8 in, invalid out\ninput: %q", content)
		}
		if d.Meta.LastActivity.IsZero() {
			t.Fatalf("no LastActivity for a readable rollout\ninput: %q", content)
		}
		if d.Meta.Tool != "codex" {
			t.Fatalf("tool tag lost: %q", d.Meta.Tool)
		}
	})
}
