package claudecode

// Fuzzing transcript digestion (docs/testing.md L2, "tolerant parsing").
//
// Transcript formats drift across tool versions and this parser is the one
// component pointed at files we do not own. The hard rule is that a malformed
// line is skipped, never fatal to the whole file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzDigestSession(f *testing.F) {
	f.Add(strings.Join([]string{
		`{"type":"user","sessionId":"s","cwd":"/r","gitBranch":"main","message":{"content":"hi"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`,
	}, "\n"))
	f.Add("")
	f.Add("\n\n\n")
	f.Add("not json at all")
	f.Add(`{"type":"user"}`)
	f.Add(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":null}]}}`)
	f.Add(`{"type":"assistant","message":{"content":"unterminated`)
	f.Add(`{"type":"user","timestamp":"not-a-date","message":{"content":"x"}}`)
	f.Add(`{"type":"user","message":{"content":[{"type":"text","text":"` + strings.Repeat("字", 5000) + `"}]}}`)
	// Deep nesting: a JSON bomb shape that recursive decoders choke on.
	f.Add(`{"type":"user","message":{"content":` + strings.Repeat("[", 200) + strings.Repeat("]", 200) + `}}`)

	f.Fuzz(func(t *testing.T, content string) {
		path := filepath.Join(t.TempDir(), "s.jsonl")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Skip()
		}
		d, err := DigestSession(path)
		if err != nil {
			// Only I/O errors are acceptable; content must never be fatal.
			t.Fatalf("digesting a readable file failed: %v\ninput: %q", err, content)
		}
		if len(d.Text) > maxDigestBytes {
			t.Fatalf("digest exceeded its budget: %d > %d bytes", len(d.Text), maxDigestBytes)
		}
		// Clipping cuts on byte offsets, so a naive implementation splits
		// multi-byte runes — Chinese prompts are the norm here. Valid input
		// must stay valid; invalid input we make no promises about.
		if utf8.ValidString(content) && !utf8.ValidString(d.Text) {
			t.Fatalf("clipping split a rune: valid UTF-8 in, invalid out\ninput: %q", content)
		}
		// Every readable file yields an observation time, or facts distilled
		// from it would fail validation downstream.
		if d.Meta.LastActivity.IsZero() {
			t.Fatalf("no LastActivity for a readable transcript\ninput: %q", content)
		}
	})
}

// A digest of one good line and one corrupt line must keep the good one:
// tolerance is not just "does not crash", it is "does not lose data".
func TestDigestKeepsGoodLinesAmongBadOnes(t *testing.T) {
	cases := []string{
		"{{{ not json",
		`{"type":"assistant","message":{"content":`,
		"\x00\x01\x02",
		strings.Repeat("x", 100_000),
	}
	for _, bad := range cases {
		path := filepath.Join(t.TempDir(), "s.jsonl")
		content := strings.Join([]string{
			`{"type":"user","sessionId":"keepme","message":{"content":"the good line"}}`,
			bad,
			`{"type":"assistant","message":{"content":"the other good line"}}`,
		}, "\n")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		d, err := DigestSession(path)
		if err != nil {
			t.Fatalf("one bad line failed the whole file: %v", err)
		}
		for _, want := range []string{"the good line", "the other good line"} {
			if !strings.Contains(d.Text, want) {
				t.Errorf("lost %q to a neighbouring bad line %.20q\ndigest: %s", want, bad, d.Text)
			}
		}
		if d.Meta.SessionID != "keepme" {
			t.Errorf("lost session metadata to a bad line %.20q", bad)
		}
	}
}
