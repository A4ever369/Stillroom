package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --transcript pointed at a directory distills every .jsonl under it in one
// command — the batch case (a folder of exported traces), not one file at a time.
func TestDistillTranscriptDirDistillsWholeFolder(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	w.fakeClaude(proposal(okProposal))

	dir := t.TempDir()
	body := []byte(strings.Join(longSession(), "\n"))
	for _, name := range []string{"a.jsonl", "b.jsonl", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r := w.run("distill", "--dry-run", "--transcript", dir)
	if r.code != 0 {
		t.Fatalf("distill --transcript <dir> exit %d\n%s", r.code, r.out())
	}
	// Two .jsonl files distilled; the .txt is ignored.
	if !strings.Contains(r.stdout, "2 session(s) to distill") {
		t.Errorf("missing the folder batch heads-up:\n%s", r.stdout)
	}
	if n := strings.Count(r.stdout, "distilling "); n != 2 {
		t.Errorf("expected 2 sessions distilled from the folder, saw %d\n%s", n, r.stdout)
	}
}

// --limit caps how many sessions a run distills — the guardrail against a
// first run over weeks of history firing an unbounded number of paid model
// calls. Uses --dry-run so nothing is written or ledgered and the run is
// repeatable.
func TestDistillLimitCapsTheRun(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	w.fakeClaude(proposal(okProposal))
	w.session("a.jsonl", longSession()...)
	w.session("b.jsonl", longSession()...)
	w.session("c.jsonl", longSession()...)

	r := w.run("distill", "--dry-run", "--limit", "1")
	if r.code != 0 {
		t.Fatalf("distill --limit exit %d\n%s", r.code, r.out())
	}
	if !strings.Contains(r.stdout, "3 sessions pending; processing the 1 most recent") {
		t.Errorf("missing the cost/limit heads-up:\n%s", r.stdout)
	}
	if n := strings.Count(r.stdout, "distilling "); n != 1 {
		t.Errorf("expected exactly 1 session distilled, saw %d\n%s", n, r.stdout)
	}
}

// Without --limit the run reports the total up front so the cost is visible.
func TestDistillWithoutLimitAnnouncesCount(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	w.fakeClaude(proposal(okProposal))
	w.session("a.jsonl", longSession()...)
	w.session("b.jsonl", longSession()...)

	r := w.run("distill", "--dry-run")
	if !strings.Contains(r.stdout, "2 session(s) to distill") {
		t.Errorf("missing the up-front count:\n%s", r.stdout)
	}
	if strings.Contains(r.stdout, "tip: use --limit") {
		t.Errorf("tip should only appear past the threshold, not for 2 sessions:\n%s", r.stdout)
	}
}
