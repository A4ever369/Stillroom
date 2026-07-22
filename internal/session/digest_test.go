package session

import (
	"strings"
	"testing"
)

func TestClipKeepsHeadAndTailOnRuneBoundaries(t *testing.T) {
	s := strings.Repeat("中文内容一二三四五", 1000) // 15KB of multi-byte text
	out := Clip(s, 1000)
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

func TestClipLeavesShortStringsAlone(t *testing.T) {
	s := "short enough"
	if got := Clip(s, 1000); got != s {
		t.Errorf("Clip mangled a sub-budget string: %q", got)
	}
}

// When max is smaller than the elision marker there is no room to keep a tail;
// Clip must still cut on a rune boundary and never exceed the budget.
func TestClipWithTinyBudgetStaysOnRuneBoundary(t *testing.T) {
	s := strings.Repeat("中", 50) // 150 bytes, 3 bytes per rune
	out := Clip(s, 7)            // below the marker length → the keep<=0 branch
	if len(out) > 7 {
		t.Fatalf("tiny-budget clip exceeded max: %d bytes", len(out))
	}
	if !utf8ValidWholeRunes(out) {
		t.Fatalf("tiny-budget clip split a rune: %q", out)
	}
}

func utf8ValidWholeRunes(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

func TestWriteTurnCountsAndSkipsEmpty(t *testing.T) {
	var b strings.Builder
	turns := 0
	WriteTurn(&b, "user", "  hello  ", &turns)
	WriteTurn(&b, "assistant", "   ", &turns) // whitespace only — dropped, not counted
	WriteTurn(&b, "tool:Bash", "make deploy", &turns)
	if turns != 2 {
		t.Errorf("turns = %d, want 2 (empty turn must not count)", turns)
	}
	got := b.String()
	if !strings.Contains(got, "[user] hello\n") || !strings.Contains(got, "[tool:Bash] make deploy\n") {
		t.Errorf("unexpected rendering:\n%s", got)
	}
	if strings.Contains(got, "[assistant]") {
		t.Errorf("empty turn leaked into output:\n%s", got)
	}
}

func TestCompactJSONAndAny(t *testing.T) {
	if got := CompactJSON(map[string]any{"command": "make"}, 300); got != `{"command":"make"}` {
		t.Errorf("CompactJSON = %q", got)
	}
	if got := CompactAny("plain string", 300); got != "plain string" {
		t.Errorf("CompactAny(string) = %q", got)
	}
	if got := CompactAny([]any{"a", "b"}, 300); got != `["a","b"]` {
		t.Errorf("CompactAny(slice) = %q", got)
	}
}
