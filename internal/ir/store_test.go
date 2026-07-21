package ir

// Store layout, init/upgrade, and the playbook load/write paths (docs/testing.md
// L1: "补 ir store 的错误路径"). The fact paths are exercised in ir_test.go and
// invariants_test.go; this file fills the playbook and init/upgrade gaps.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) Store {
	t.Helper()
	return Store{Root: t.TempDir()}
}

func TestExistsTracksInit(t *testing.T) {
	s := newStore(t)
	if s.Exists() {
		t.Fatal("a bare directory should not look initialized")
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if !s.Exists() {
		t.Fatal("Exists should be true after Init")
	}
}

func TestInitLaysOutTheStore(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	// Empty knowledge dirs must ship a keeper or git drops them and a fresh
	// clone crashes on its first distill (regression: the onboarding bug).
	for _, want := range []string{
		filepath.Join(s.FactsDir(), ".gitkeep"),
		filepath.Join(s.PlaybooksDir(), ".gitkeep"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("Init did not create %s: %v", want, err)
		}
	}
	assertFileContains(t, filepath.Join(s.Dir(), ".gitignore"), "queue/")
	assertFileContains(t, filepath.Join(s.Dir(), ".gitignore"), ".local/")
	assertFileContains(t, filepath.Join(s.Dir(), ".gitattributes"), "materialized.md merge=union")
}

func TestInitIsIdempotent(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, filepath.Join(s.Dir(), ".gitignore"))
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	after := readFile(t, filepath.Join(s.Dir(), ".gitignore"))
	if before != after {
		t.Errorf("second Init rewrote .gitignore\nbefore: %q\nafter:  %q", before, after)
	}
}

// ensureLines is the upgrade-in-place mechanism: a repo initialized by an older
// version that already hand-edited .gitignore must GAIN the new rules without
// losing its own lines, and without a spurious rewrite when nothing is missing.
func TestEnsureLinesUpgradesInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	// Pre-existing file, user content, deliberately no trailing newline.
	if err := os.WriteFile(path, []byte("*.log\nnode_modules/"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureLines(path, "queue/", ".local/"); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	for _, want := range []string{"*.log", "node_modules/", "queue/", ".local/"} {
		if !containsLine(got, want) {
			t.Errorf("upgrade lost or missed %q\nresult: %q", want, got)
		}
	}
	// Re-running with a line already present must not append a duplicate.
	if err := ensureLines(path, "queue/"); err != nil {
		t.Fatal(err)
	}
	if second := readFile(t, path); second != got {
		t.Errorf("ensureLines re-appended an already-present line\nbefore: %q\nafter:  %q", got, second)
	}
}

func TestLoadPlaybooksMissingDirIsEmpty(t *testing.T) {
	s := newStore(t) // never Init'd
	pbs, bad, err := s.LoadPlaybooks()
	if err != nil {
		t.Fatalf("missing playbooks dir should be empty, not an error: %v", err)
	}
	if len(pbs) != 0 || bad != nil {
		t.Errorf("expected empty load, got %d playbooks / bad=%v", len(pbs), bad)
	}
}

func TestWriteThenLoadPlaybookRoundTrips(t *testing.T) {
	s := newStore(t)
	p := Playbook{
		ID: "acme-deploy", Title: "Deploy Acme",
		Sources:   []string{"claude-code://s1"},
		UpdatedAt: time.Unix(1700000000, 0).UTC(),
		Body:      "## Steps\n1. make deploy-prod",
	}
	// WritePlaybook must MkdirAll: a fresh store has no playbooks/ dir yet
	// unless Init ran, and Apply writes without a prior Init in some paths.
	if err := s.WritePlaybook(p); err != nil {
		t.Fatal(err)
	}
	pbs, bad, err := s.LoadPlaybooks()
	if err != nil || bad != nil {
		t.Fatalf("load failed: err=%v bad=%v", err, bad)
	}
	if len(pbs) != 1 || pbs[0].ID != "acme-deploy" || pbs[0].Title != "Deploy Acme" {
		t.Fatalf("round trip lost data: %+v", pbs)
	}
}

func TestWritePlaybookRejectsInvalid(t *testing.T) {
	s := newStore(t)
	// Missing title — Validate must refuse before anything touches disk.
	err := s.WritePlaybook(Playbook{ID: "x", UpdatedAt: time.Now(), Body: "b"})
	if err == nil {
		t.Fatal("WritePlaybook accepted a playbook with no title")
	}
	if _, statErr := os.Stat(s.PlaybooksDir()); statErr == nil {
		if entries, _ := os.ReadDir(s.PlaybooksDir()); len(entries) > 0 {
			t.Errorf("a rejected playbook still wrote files: %v", entries)
		}
	}
}

func TestLoadPlaybooksIsolatesBadFiles(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(s.PlaybooksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	good := Playbook{ID: "good", Title: "Good", UpdatedAt: time.Unix(1700000000, 0).UTC(), Body: "## ok"}
	writeRaw(t, filepath.Join(s.PlaybooksDir(), "good.md"), string(good.Encode()))
	writeRaw(t, filepath.Join(s.PlaybooksDir(), "broken.md"), "no frontmatter here")
	writeRaw(t, filepath.Join(s.PlaybooksDir(), "notes.txt"), "ignored, not .md")

	pbs, bad, err := s.LoadPlaybooks()
	if err != nil {
		t.Fatalf("one bad file aborted the whole load: %v", err)
	}
	if len(pbs) != 1 || pbs[0].ID != "good" {
		t.Errorf("expected only the good playbook, got %+v", pbs)
	}
	if _, ok := bad["broken.md"]; !ok {
		t.Errorf("broken.md should be reported in bad, got %v", bad)
	}
	if _, ok := bad["notes.txt"]; ok {
		t.Errorf("non-.md file should be skipped, not reported as bad")
	}
}

func TestSortFactsIsDeterministic(t *testing.T) {
	older := time.Unix(1700000000, 0).UTC()
	newer := older.Add(time.Hour)
	facts := []Fact{
		{ID: "b.x", ObservedAt: older},
		{ID: "a.x", ObservedAt: older},
		{ID: "a.x", ObservedAt: newer}, // same id, newer — must sort ahead of its older twin
	}
	SortFacts(facts)
	// Primary key: id ascending. Secondary: newest observation first.
	if facts[0].ID != "a.x" || !facts[0].ObservedAt.Equal(newer) {
		t.Errorf("newest same-id fact should lead; got %s@%s", facts[0].ID, facts[0].ObservedAt)
	}
	if facts[1].ID != "a.x" || !facts[1].ObservedAt.Equal(older) {
		t.Errorf("older same-id fact should follow its newer twin; got %s@%s", facts[1].ID, facts[1].ObservedAt)
	}
	if facts[2].ID != "b.x" {
		t.Errorf("distinct id should sort by id last; got %s", facts[2].ID)
	}
}

func TestPlaybookFilename(t *testing.T) {
	p := Playbook{ID: "acme-deploy"}
	if got := p.Filename(); got != "acme-deploy.md" {
		t.Errorf("Filename() = %q, want acme-deploy.md", got)
	}
}

// --- small local helpers ---

func assertFileContains(t *testing.T, path, line string) {
	t.Helper()
	if !containsLine(readFile(t, path), line) {
		t.Errorf("%s is missing line %q", path, line)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func writeRaw(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
