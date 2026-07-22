// Package session holds the tool-agnostic session digest: the condensed,
// prompt-sized rendering of one AI coding session that the distiller consumes.
//
// Each source tool (Claude Code, Codex, …) has its own adapter under
// internal/adapter/<tool> that parses that tool's at-rest transcript format
// and produces a session.Digest. Everything downstream — redaction,
// distillation, fact stamping — is written against this one type, so adding a
// tool never touches the pipeline (docs/design-v2.md §4.2, the adapter split).
package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Meta is machine/session context lifted from a transcript. Not every field is
// available from every tool — GitBranch/Version come from Claude Code's
// per-line envelope; Codex carries neither. Empty is a valid "this tool does
// not record it" answer.
type Meta struct {
	// Tool identifies the source adapter, e.g. "claude-code" or "codex". It
	// selects the scheme of the evidence ref stamped onto facts.
	Tool      string
	SessionID string
	CWD       string
	GitBranch string
	Version   string
	Turns     int
	// LastActivity is when the session itself ended — NOT when it was
	// distilled. Facts carry this as observed_at so that supersession orders
	// knowledge by when it was learned; keying on distillation time would let
	// someone distilling a three-week-old session clobber yesterday's fact
	// merely by running the tool later.
	LastActivity time.Time
}

// Digest is a condensed, prompt-sized rendering of one session used as
// distiller input. It is NOT the evidence itself — only a lossy projection;
// the Source ref written into facts points at the real transcript.
type Digest struct {
	Meta Meta
	Text string
}

// MaxDigestBytes bounds the digest so distillation prompts stay well under
// model context limits even for 40MB sessions. When the transcript exceeds
// the budget, keep the head and tail halves: openings carry the task framing,
// endings carry the resolution — the middle is mostly search.
const MaxDigestBytes = 200_000

// WriteTurn appends one "[role] text" line and counts it as a turn. Empty text
// is dropped (and not counted). Each turn is clipped so a single runaway tool
// dump cannot dominate the digest.
func WriteTurn(b *strings.Builder, role, text string, turns *int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	*turns++
	fmt.Fprintf(b, "[%s] %s\n", role, Clip(text, 2000))
}

// CompactJSON marshals v and clips it — used to render tool-call inputs.
func CompactJSON(v any, limit int) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return Clip(string(data), limit)
}

// CompactAny renders a tool-result payload that may be a string or structured.
func CompactAny(v any, limit int) string {
	switch t := v.(type) {
	case string:
		return Clip(t, limit)
	default:
		return CompactJSON(v, limit)
	}
}

// Clip truncates s to max bytes, keeping head and tail halves around an
// elision marker. Cuts land on rune boundaries so multi-byte text (Chinese
// prompts are the norm here) never splits mid-character.
func Clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const marker = "\n[...elided...]\n"
	keep := max - len(marker)
	if keep <= 0 {
		return s[:runeBoundary(s, max)]
	}
	head := runeBoundary(s, keep/2)
	tailStart := len(s) - (keep - keep/2)
	for tailStart < len(s) && !isRuneStart(s[tailStart]) {
		tailStart++
	}
	return s[:head] + marker + s[tailStart:]
}

func runeBoundary(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	for i > 0 && !isRuneStart(s[i]) {
		i--
	}
	return i
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
