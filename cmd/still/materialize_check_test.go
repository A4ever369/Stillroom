package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// materialize --check and the doctor drift check guard a real failure mode: a
// hand edit or a merge changes facts/ but nobody re-renders, so the committed
// materialized.md — which every teammate's agent loads — goes stale.
func TestMaterializeCheckDetectsDrift(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	w.fakeClaude(proposal(okProposal))
	w.session("sess.jsonl", longSession()...)
	w.run("distill") // writes a fact and a fresh materialized.md

	if r := w.run("materialize", "--check"); r.code != 0 {
		t.Fatalf("check should pass right after distill, got %d\n%s", r.code, r.out())
	}

	// Add a fact file directly, as a merge or hand edit would, without
	// re-materializing.
	factPath := filepath.Join(w.repo, ".team-context", "facts", "manual.fact.md")
	body := "---\nid: manual.fact\nscope: repo:x\nobserved_at: 2026-07-21T00:00:00Z\n" +
		"source: claude-code://s\nconfidence: high\nstatus: active\n---\nadded by hand\n"
	if err := os.WriteFile(factPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	r := w.run("materialize", "--check")
	if r.code == 0 {
		t.Errorf("check should fail on stale materialized.md, exited 0\n%s", r.out())
	}
	if !strings.Contains(r.out(), "stale") {
		t.Errorf("stale message expected, got:\n%s", r.out())
	}

	// doctor must surface the same drift (and therefore exit non-zero).
	if d := w.run("doctor"); d.code == 0 || !strings.Contains(d.out(), "materialized.md is up to date") {
		t.Errorf("doctor should flag the drift:\n%s", d.out())
	}

	// Re-rendering clears it.
	w.run("materialize")
	if r := w.run("materialize", "--check"); r.code != 0 {
		t.Errorf("check should pass after re-materialize, got %d\n%s", r.code, r.out())
	}
}
