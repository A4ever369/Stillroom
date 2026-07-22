// Package codex is the OpenAI Codex CLI adapter: it turns a Codex "rollout"
// transcript into the tool-agnostic session.Digest the distiller consumes.
//
// Codex stores each session as ~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl.
// Every line is {"timestamp","type","payload"}; the shapes below are lifted
// from real rollout files. As with the Claude Code adapter, at-rest parsing is
// version-fragile (design-v2 §11.4) — tolerate drift, never fail a whole file
// on one bad line.
package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/A4ever369/Stillroom/internal/session"
)

// Tool is the source-adapter identity stamped onto digests and evidence refs.
const Tool = "codex"

// DigestSession reads a Codex rollout file and produces a distiller-ready
// digest, mirroring the tolerance contract of the Claude Code adapter.
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
		var entry struct {
			Timestamp string          `json:"timestamp"`
			Type      string          `json:"type"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		absorbTimestamp(&d.Meta, entry.Timestamp)
		switch entry.Type {
		case "session_meta":
			absorbMeta(&d.Meta, entry.Payload)
		case "response_item":
			renderResponseItem(&b, entry.Payload, &d.Meta.Turns)
			// event_msg / turn_context / world_state are telemetry and framing;
			// the conversation itself lives entirely in response_item.
		}
	}
	if err := sc.Err(); err != nil {
		return d, err
	}
	if d.Meta.LastActivity.IsZero() {
		if info, err := f.Stat(); err == nil {
			d.Meta.LastActivity = info.ModTime().UTC()
		}
	}
	d.Text = session.Clip(b.String(), session.MaxDigestBytes)
	return d, nil
}

func absorbMeta(m *session.Meta, payload json.RawMessage) {
	var meta struct {
		SessionID string `json:"session_id"`
		ID        string `json:"id"`
		CWD       string `json:"cwd"`
	}
	if err := json.Unmarshal(payload, &meta); err != nil {
		return
	}
	if m.SessionID == "" {
		if meta.SessionID != "" {
			m.SessionID = meta.SessionID
		} else {
			m.SessionID = meta.ID
		}
	}
	if m.CWD == "" {
		m.CWD = meta.CWD
	}
}

func absorbTimestamp(m *session.Meta, ts string) {
	if ts == "" {
		return
	}
	// Codex writes millisecond-precision RFC3339 ("…T07:24:20.533Z"); Go's
	// RFC3339 layout accepts the fractional part. Keep the latest seen.
	if t, err := time.Parse(time.RFC3339, ts); err == nil && t.After(m.LastActivity) {
		m.LastActivity = t.UTC()
	}
}

// renderResponseItem renders one response_item payload. Messages become turns;
// tool calls and their outputs are kept (they carry the real work); reasoning
// blocks are dropped, matching the Claude Code adapter's treatment of thinking.
func renderResponseItem(b *strings.Builder, payload json.RawMessage, turns *int) {
	var item struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Name    string `json:"name"`
		Args    string `json:"arguments"`
		Output  any    `json:"output"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(payload, &item); err != nil {
		return
	}
	switch item.Type {
	case "message":
		// Only real conversation. Codex injects large "developer" messages
		// (environment, approval policy, base instructions) that are framing,
		// not knowledge — the same reason the Claude Code adapter renders only
		// user/assistant. Dropping them also keeps the digest budget for signal.
		if item.Role != "user" && item.Role != "assistant" {
			return
		}
		for _, c := range item.Content {
			// Codex content items are input_text (user) / output_text (assistant).
			if c.Text != "" {
				session.WriteTurn(b, item.Role, c.Text, turns)
			}
		}
	case "function_call":
		name := item.Name
		if name == "" {
			name = "call"
		}
		session.WriteTurn(b, "tool:"+name, session.Clip(item.Args, 300), turns)
	case "function_call_output":
		session.WriteTurn(b, "result", session.CompactAny(item.Output, 300), turns)
	}
}
