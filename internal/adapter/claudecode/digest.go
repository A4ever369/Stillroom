package claudecode

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/A4ever369/Stillroom/internal/session"
)

// Tool is the source-adapter identity stamped onto every digest and evidence
// ref produced here.
const Tool = "claude-code"

// DigestSession reads a Claude Code transcript and produces a
// distiller-ready digest. Malformed lines are skipped, matching
// ParseClaudeSession's tolerance contract.
//
// Claude Code embeds cwd/gitBranch/version on individual lines (verified
// at-rest format, docs/session-ir-research-brief.md §6).
func DigestSession(path string) (session.Digest, error) {
	f, err := os.Open(path)
	if err != nil {
		return session.Digest{}, err
	}
	defer f.Close()

	var d session.Digest
	d.Meta.Tool = Tool
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
	if d.Meta.LastActivity.IsZero() {
		// Older transcripts (and some tool versions) carry no per-line
		// timestamp; the file's mtime is the best remaining proxy for when
		// the session ended.
		if info, err := f.Stat(); err == nil {
			d.Meta.LastActivity = info.ModTime().UTC()
		}
	}
	d.Text = session.Clip(b.String(), session.MaxDigestBytes)
	return d, nil
}

func absorbMeta(m *session.Meta, raw map[string]any) {
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
	// Keep the latest timestamp seen rather than the last one parsed:
	// transcripts are usually chronological, but resumed sessions and
	// sidechains are not guaranteed to be.
	if v, ok := raw["timestamp"].(string); ok {
		if ts, err := time.Parse(time.RFC3339, v); err == nil && ts.After(m.LastActivity) {
			m.LastActivity = ts.UTC()
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
			session.WriteTurn(b, role, txt, turns)
		}
		return
	}
	switch content := msg["content"].(type) {
	case string:
		session.WriteTurn(b, role, content, turns)
	case []any:
		for _, blk := range content {
			block, ok := blk.(map[string]any)
			if !ok {
				continue
			}
			switch bt, _ := block["type"].(string); bt {
			case "text":
				if txt, _ := block["text"].(string); txt != "" {
					session.WriteTurn(b, role, txt, turns)
				}
			case "tool_use":
				name, _ := block["name"].(string)
				input := session.CompactJSON(block["input"], 300)
				session.WriteTurn(b, "tool:"+name, input, turns)
			case "tool_result":
				session.WriteTurn(b, "result", session.CompactAny(block["content"], 300), turns)
				// thinking blocks are deliberately dropped: they are the noisiest
				// layer and the least transferable (research brief Q1).
			}
		}
	}
}
