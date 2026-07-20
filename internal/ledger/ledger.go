// Package ledger tracks which sessions have already been distilled, so
// auto-discovery never re-distills the same transcript. The ledger lives in
// .team-context/.local/ — machine-private and gitignored, because transcript
// paths are machine state, not team knowledge.
package ledger

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const fileName = "distilled.jsonl"

// Entry records one completed distillation.
type Entry struct {
	Transcript  string `json:"transcript"`
	SessionID   string `json:"session_id,omitempty"`
	DistilledAt string `json:"distilled_at"`
	// Facts is how many facts the run produced (0 = uneventful session;
	// still recorded so we don't retry it forever).
	Facts int `json:"facts"`
}

// Ledger is an append-only record in dir (typically .team-context/.local).
type Ledger struct {
	dir string
}

// Open returns the ledger stored in dir, creating nothing until Mark.
func Open(dir string) Ledger { return Ledger{dir: dir} }

func (l Ledger) path() string { return filepath.Join(l.dir, fileName) }

// Seen reports whether the transcript path was already distilled.
func (l Ledger) Seen(transcript string) bool {
	f, err := os.Open(l.path())
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal([]byte(strings.TrimSpace(sc.Text())), &e); err != nil {
			continue
		}
		if e.Transcript == transcript {
			return true
		}
	}
	return false
}

// Mark appends a completed distillation. Append-only by design: history is
// cheap and a corrupted tail loses at most one line.
func (l Ledger) Mark(e Entry) error {
	if e.DistilledAt == "" {
		e.DistilledAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(l.path(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}
