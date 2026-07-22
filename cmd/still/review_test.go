package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// still review is the §13 PR-comment renderer. Black-box it through the two
// dir flags so no git plumbing is needed: it must classify a new fact and emit
// the stable marker a CI workflow keys its comment on.
func TestReviewRendersKnowledgeDiff(t *testing.T) {
	w := newWorld(t)

	base := filepath.Join(t.TempDir(), "base")
	head := filepath.Join(t.TempDir(), "head")
	writeFactFile(t, base, "deploy.acme.db", "2026-07-20T00:00:00Z", "DB entry is pgbouncer on 6432.")
	writeFactFile(t, head, "deploy.acme.db", "2026-07-20T00:00:00Z", "DB entry is pgbouncer on 6432.")
	writeFactFile(t, head, "ci.pg.image", "2026-07-21T00:00:00Z", "CI must use pgvector image.")

	r := w.run("review", "--base", base, "--head", head)
	if r.code != 0 {
		t.Fatalf("review exit %d\nstderr: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "<!-- stillroom-knowledge-diff -->") {
		t.Error("output missing the stable comment marker")
	}
	if !strings.Contains(r.stdout, "➕ 1 new") || !strings.Contains(r.stdout, "ci.pg.image") {
		t.Errorf("did not report the new fact:\n%s", r.stdout)
	}
	if strings.Contains(r.stdout, "deploy.acme.db") {
		t.Errorf("unchanged fact should not appear:\n%s", r.stdout)
	}
}

// With no --base, everything under --head is new — the first-adoption case.
func TestReviewWithNoBaseTreatsAllAsNew(t *testing.T) {
	w := newWorld(t)
	head := filepath.Join(t.TempDir(), "head")
	writeFactFile(t, head, "a.b", "2026-07-21T00:00:00Z", "first fact")

	r := w.run("review", "--head", head)
	if r.code != 0 {
		t.Fatalf("review exit %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "➕ 1 new") {
		t.Errorf("empty base should make the fact new:\n%s", r.stdout)
	}
}

func writeFactFile(t *testing.T, root, id, observed, body string) {
	t.Helper()
	dir := filepath.Join(root, ".team-context", "facts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nid: " + id + "\nscope: repo:x\nobserved_at: " + observed +
		"\nsource: claude-code://s\nconfidence: high\nstatus: active\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
