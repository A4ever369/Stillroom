// Package queue is the tiny pending-transcript queue the SessionEnd hook
// writes into and `still distill` drains. Entries are pointer files (the
// transcript path, nothing else) named by path hash, so re-enqueueing a
// resumed session is idempotent and nothing sensitive is duplicated.
// The queue directory is machine-private and gitignored.
package queue

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

func entryName(transcript string) string {
	sum := sha256.Sum256([]byte(transcript))
	return hex.EncodeToString(sum[:8]) + ".path"
}

// Enqueue records a transcript path. Creating the dir on demand keeps the
// hook safe to run in repos initialized by older versions.
func Enqueue(dir, transcript string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, entryName(transcript)), []byte(transcript), 0o644)
}

// List returns queued transcript paths whose files still exist on disk.
// Entries pointing at deleted transcripts are pruned as they are seen.
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".path") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		p := strings.TrimSpace(string(data))
		if _, err := os.Stat(p); err != nil {
			_ = os.Remove(filepath.Join(dir, e.Name())) // prune dangling entry
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// Remove drops the entry for a transcript (after successful distillation).
func Remove(dir, transcript string) {
	_ = os.Remove(filepath.Join(dir, entryName(transcript)))
}
