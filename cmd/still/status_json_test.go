package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// still status --json is the machine-readable surface for CI and editor
// integrations. It must be valid JSON, never-null arrays, and agree with what
// the text mode reports.
func TestStatusJSONReport(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	w.fakeClaude(proposal(okProposal))
	w.session("sess.jsonl", longSession()...)
	w.run("distill")

	r := w.run("status", "--json")
	if r.code != 0 {
		t.Fatalf("status --json exit %d\n%s", r.code, r.out())
	}

	var rep struct {
		Facts     struct{ Total, Active, Bad int } `json:"facts"`
		Playbooks struct {
			Total, Bad int
		} `json:"playbooks"`
		PendingSessions int      `json:"pending_sessions"`
		BadFiles        []string `json:"bad_files"`
		MaterializedUp  bool     `json:"materialized_up_to_date"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, r.stdout)
	}
	if rep.Facts.Total != 1 || rep.Facts.Active != 1 || rep.Facts.Bad != 0 {
		t.Errorf("facts = %+v", rep.Facts)
	}
	if rep.PendingSessions != 0 {
		t.Errorf("pending should be 0 after distill, got %d", rep.PendingSessions)
	}
	if !rep.MaterializedUp {
		t.Error("materialized should be current right after distill")
	}
	if rep.BadFiles == nil {
		t.Error("bad_files must be [] not null")
	}
}

// A broken fact file must surface in both the count and the bad_files list.
func TestStatusJSONReportsBadFiles(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	broken := filepath.Join(w.repo, ".team-context", "facts", "broken.md")
	if err := os.WriteFile(broken, []byte("not a fact"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := w.run("status", "--json")
	var rep struct {
		Facts    struct{ Bad int } `json:"facts"`
		BadFiles []string          `json:"bad_files"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, r.stdout)
	}
	if rep.Facts.Bad != 1 {
		t.Errorf("expected 1 bad fact, got %d", rep.Facts.Bad)
	}
	if len(rep.BadFiles) != 1 || rep.BadFiles[0] != ".team-context/facts/broken.md" {
		t.Errorf("bad_files = %v", rep.BadFiles)
	}
}
