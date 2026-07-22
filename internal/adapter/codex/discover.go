package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Session is one discovered at-rest Codex rollout transcript.
type Session struct {
	Path    string
	Size    int64
	ModUnix int64 // mtime, unix seconds — newest-first ordering
}

// Home returns the Codex config dir: $CODEX_HOME or ~/.codex.
func Home() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// IsRollout reports whether a path looks like a Codex rollout file, used to
// dispatch a queued transcript path to the right adapter.
func IsRollout(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, "rollout-") && strings.HasSuffix(base, ".jsonl")
}

// Discover lists the Codex rollouts whose session belongs to cwd, newest
// first. Unlike Claude Code (which encodes cwd into the directory name), Codex
// stores sessions in a date tree and records cwd inside the file, so we must
// read each rollout's session_meta line to match. A missing sessions dir
// yields an empty list, never an error.
func Discover(home, cwd string) ([]Session, error) {
	root := filepath.Join(home, "sessions")
	var out []Session
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			// A permission error on one subtree must not abort discovery.
			if os.IsNotExist(err) {
				return nil
			}
			return nil
		}
		if e.IsDir() || !IsRollout(path) {
			return nil
		}
		if sessionCWD(path) != cwd {
			return nil
		}
		info, ierr := e.Info()
		if ierr != nil {
			return nil
		}
		out = append(out, Session{Path: path, Size: info.Size(), ModUnix: info.ModTime().Unix()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModUnix > out[j].ModUnix })
	return out, nil
}

// sessionCWD reads a rollout's leading lines for its session_meta cwd. It
// stops early: session_meta is the first record in practice, so scanning a
// handful of lines avoids opening the whole (possibly huge) file.
func sessionCWD(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for i := 0; sc.Scan() && i < 10; i++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var entry struct {
			Type    string `json:"type"`
			Payload struct {
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type == "session_meta" {
			return entry.Payload.CWD
		}
	}
	return ""
}
