package main

// Black-box CLI harness. Every case gets an isolated world: a throwaway git
// repo as cwd, a throwaway CLAUDE_CONFIG_DIR holding synthetic transcripts,
// and a fake `claude` on PATH — so the whole CLI is exercised end to end
// without spending a token or touching the developer's real home.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/A4ever369/Stillroom/internal/adapter/claudecode"
)

var stillBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "stillroom-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness:", err)
		os.Exit(1)
	}
	stillBin = filepath.Join(dir, "still")
	build := exec.Command("go", "build", "-o", stillBin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "harness: build failed: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type world struct {
	t          *testing.T
	repo       string
	claudeHome string
	binDir     string
}

// newWorld builds an isolated repo + fake Claude Code home. Paths are
// symlink-resolved because the child process sees the resolved cwd, and
// discovery encodes that exact string into the storage directory name.
func newWorld(t *testing.T) *world {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &world{
		t:          t,
		repo:       filepath.Join(base, "repo"),
		claudeHome: filepath.Join(base, "claude-home"),
		binDir:     filepath.Join(base, "bin"),
	}
	for _, d := range []string{w.repo, w.claudeHome, w.binDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// -b main pins the branch name so merge tests do not depend on the
	// host's init.defaultBranch.
	if out, err := exec.Command("git", "-C", w.repo, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return w
}

// clone produces a second working copy of this world's repo, as a teammate
// would have. It shares the fake toolchain but gets its own repo path — and
// therefore its own session-discovery directory, since discovery is keyed on
// the encoded cwd.
func (w *world) clone(name string) *world {
	w.t.Helper()
	dst := filepath.Join(filepath.Dir(w.repo), name)
	if out, err := exec.Command("git", "clone", "-q", w.repo, dst).CombinedOutput(); err != nil {
		w.t.Fatalf("git clone: %v\n%s", err, out)
	}
	return &world{t: w.t, repo: dst, claudeHome: w.claudeHome, binDir: w.binDir}
}

// pullFrom merges another world's main branch, returning the merge output and
// whether git reported a conflict.
func (w *world) pullFrom(other *world) (string, bool) {
	w.t.Helper()
	out, err := exec.Command("git", "-C", w.repo,
		"-c", "user.email=test@stillroom.invalid",
		"-c", "user.name=stillroom test",
		"pull", "--no-rebase", "-q", other.repo, "main",
	).CombinedOutput()
	return string(out), err != nil
}

type result struct {
	code   int
	stdout string
	stderr string
}

func (r result) out() string { return r.stdout + r.stderr }

// run invokes the built binary inside the world. cwd defaults to the repo.
func (w *world) run(args ...string) result {
	w.t.Helper()
	return w.runIn(w.repo, "", args...)
}

func (w *world) runIn(cwd, stdin string, args ...string) result {
	w.t.Helper()
	cmd := exec.Command(stillBin, args...)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// PATH is the fake bin dir ALONE, never the developer's: a case that
	// installs no fake `claude` must see a machine without one, whatever is
	// on the real host. The CLI shells out to nothing else.
	cmd.Env = []string{
		"CLAUDE_CONFIG_DIR=" + w.claudeHome,
		"PATH=" + w.binDir,
		"HOME=" + filepath.Dir(w.repo),
	}
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		w.t.Fatalf("run %v: %v", args, err)
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// fakeClaude installs a `claude` that ignores its input and prints the given
// stdout verbatim, letting a case script any distiller response — including
// malformed ones, which the real CLI can never be made to produce on demand.
func (w *world) fakeClaude(stdout string) {
	w.t.Helper()
	// Absolute paths throughout: PATH holds only this directory, so the fake
	// cannot rely on finding its own utilities.
	script := "#!/bin/sh\n/bin/cat > /dev/null\n/bin/cat <<'STILLROOM_EOF'\n" + stdout + "\nSTILLROOM_EOF\n"
	w.writeExec(filepath.Join(w.binDir, "claude"), script)
}

// fakeClaudeFailing installs a `claude` that exits non-zero.
func (w *world) fakeClaudeFailing() {
	w.t.Helper()
	w.writeExec(filepath.Join(w.binDir, "claude"), "#!/bin/sh\n/bin/cat > /dev/null\necho 'boom' >&2\nexit 3\n")
}

func (w *world) writeExec(path, body string) {
	w.t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		w.t.Fatal(err)
	}
}

// proposal wraps a distiller JSON proposal in the `claude -p --output-format
// json` envelope the runner expects.
func proposal(inner string) string {
	return `{"result": ` + jsonString(inner) + `}`
}

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// session writes a synthetic transcript into the location Claude Code would
// have stored it for this repo, so auto-discovery finds it.
func (w *world) session(name string, lines ...string) string {
	w.t.Helper()
	dir := filepath.Join(w.claudeHome, "projects", claudecode.EncodeProjectDir(w.repo))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		w.t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		w.t.Fatal(err)
	}
	return path
}

// longSession returns transcript lines with enough turns to clear minTurns.
func longSession() []string {
	return []string{
		`{"type":"user","sessionId":"sess-abc","cwd":"/r","gitBranch":"main","message":{"content":"deploy the acme env"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"checking the db entry"},{"type":"tool_use","name":"Bash","input":{"command":"psql -p 5432"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"timeout: blocked"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"use pgbouncer on 6432"}]}}`,
		`{"type":"user","message":{"content":"ok ship it"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}`,
	}
}

const okProposal = `{"facts":[{"id":"deploy.acme.db-endpoint","confidence":"high","body":"Acme prod DB entry is pgbouncer on 6432; direct 5432 is blocked."}],"playbook":{"id":"acme-deploy","title":"Acme deploy","body":"## Steps\n1. make deploy-prod"}}`

// exists reports whether a path inside the repo exists.
func (w *world) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(w.repo, rel))
	return err == nil
}

func (w *world) read(rel string) string {
	w.t.Helper()
	data, err := os.ReadFile(filepath.Join(w.repo, rel))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(data)
}
