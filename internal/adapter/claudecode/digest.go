package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SessionMeta is machine/session context lifted from a Claude Code transcript.
// Claude Code embeds cwd/gitBranch/version on individual lines (verified
// at-rest format, docs/session-ir-research-brief.md §6).
type SessionMeta struct {
	SessionID string
	CWD       string
	GitBranch string
	Version   string
	Turns     int
}

// Digest is a condensed, prompt-sized rendering of one session used as
// distiller input. It is NOT the evidence itself — only a lossy projection;
// the Source ref written into facts points at the real transcript.
type Digest struct {
	Meta SessionMeta
	Text string
}

// maxDigestBytes bounds the digest so distillation prompts stay well under
// model context limits even for 40MB sessions. When the transcript exceeds
// the budget, the digest keeps the head and tail halves: openings carry the
// task framing, endings carry the resolution — the middle is mostly search.
const maxDigestBytes = 200_000

// DigestSession reads a Claude Code transcript and produces a
// distiller-ready digest. Malformed lines are skipped, matching
// ParseClaudeSession's tolerance contract.
func DigestSession(path string) (Digest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Digest{}, err
	}
	defer f.Close()

	var d Digest
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		absorbMeta(&d.Meta, raw)
		renderLine(&b, raw, &d.Meta.Turns)
	}
	if err := sc.Err(); err != nil {
		return d, err
	}
	d.Text = clip(b.String(), maxDigestBytes)
	return d, nil
}

func absorbMeta(m *SessionMeta, raw map[string]any) {
	if m.SessionID == "" {
		if v, ok := raw["sessionId"].(string); ok {
			m.SessionID = v
		}
	}
	if m.CWD == "" {
		if v, ok := raw["cwd"].(string); ok {
			m.CWD = v
		}
	}
	if m.GitBranch == "" {
		if v, ok := raw["gitBranch"].(string); ok {
			m.GitBranch = v
		}
	}
	if m.Version == "" {
		if v, ok := raw["version"].(string); ok {
			m.Version = v
		}
	}
}

// renderLine appends a compact one-entry rendering of a transcript line.
// It understands both flat entries ({"type":"text","text":...}) and the
// message envelope shape ({"type":"assistant","message":{"content":[...]}}).
func renderLine(b *strings.Builder, raw map[string]any, turns *int) {
	role, _ := raw["type"].(string)
	switch role {
	case "user", "assistant":
	default:
		return
	}
	msg, _ := raw["message"].(map[string]any)
	if msg == nil {
		// Flat shape: {"type":"user","text":"..."}
		if txt, ok := raw["text"].(string); ok && txt != "" {
			writeTurn(b, role, txt, turns)
		}
		return
	}
	switch content := msg["content"].(type) {
	case string:
		writeTurn(b, role, content, turns)
	case []any:
		for _, blk := range content {
			block, ok := blk.(map[string]any)
			if !ok {
				continue
			}
			switch bt, _ := block["type"].(string); bt {
			case "text":
				if txt, _ := block["text"].(string); txt != "" {
					writeTurn(b, role, txt, turns)
				}
			case "tool_use":
				name, _ := block["name"].(string)
				input := compactJSON(block["input"], 300)
				writeTurn(b, "tool:"+name, input, turns)
			case "tool_result":
				writeTurn(b, "result", compactAny(block["content"], 300), turns)
				// thinking blocks are deliberately dropped: they are the noisiest
				// layer and the least transferable (research brief Q1).
			}
		}
	}
}

func writeTurn(b *strings.Builder, role, text string, turns *int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	*turns++
	fmt.Fprintf(b, "[%s] %s\n", role, clip(text, 2000))
}

func compactJSON(v any, limit int) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return clip(string(data), limit)
}

func compactAny(v any, limit int) string {
	switch t := v.(type) {
	case string:
		return clip(t, limit)
	default:
		return compactJSON(v, limit)
	}
}

// clip truncates s to max bytes, keeping head and tail halves around an
// elision marker. Cuts land on rune boundaries so multi-byte text (Chinese
// prompts are the norm here) never splits mid-character.
func clip(s string, max int) string {
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
