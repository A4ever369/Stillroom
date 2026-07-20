package claudecode

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Session is one discovered at-rest Claude Code transcript.
type Session struct {
	Path    string
	Size    int64
	ModUnix int64 // mtime, unix seconds — used for newest-first ordering
}

// Home returns the Claude Code config dir: $CLAUDE_CONFIG_DIR or ~/.claude.
func Home() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// EncodeProjectDir maps a working directory to Claude Code's per-project
// storage directory name: every non-alphanumeric byte becomes '-'
// (e.g. /Users/a/code/x → -Users-a-code-x). This mirrors the documented
// at-rest layout; treat it as version-fragile and prefer hook-provided
// transcript paths when available (design-v2 §11.4).
func EncodeProjectDir(cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// Discover lists the transcripts Claude Code has stored for the given
// working directory, newest first. A missing directory yields an empty
// list, never an error — "no sessions yet" is a normal state.
func Discover(home, cwd string) ([]Session, error) {
	dir := filepath.Join(home, "projects", EncodeProjectDir(cwd))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Session{
			Path:    filepath.Join(dir, e.Name()),
			Size:    info.Size(),
			ModUnix: info.ModTime().Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModUnix > out[j].ModUnix })
	return out, nil
}
