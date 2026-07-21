package materialize

// The tolerant-render and import-append branches (docs/testing.md L1 fill):
// a corrupt fact file must not fail the render — it must surface as a warning
// comment — and EnsureImport must not weld its line onto an unterminated file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWarnsAboutUnparseableFacts(t *testing.T) {
	s := seededStore(t)
	// Drop a corrupt fact file straight into the store. LoadFacts reports it
	// in the bad map; Run must render a warning rather than abort or omit it.
	if err := os.WriteFile(filepath.Join(s.FactsDir(), "corrupt.md"), []byte("not a fact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(s); err != nil {
		t.Fatalf("a corrupt fact file should not fail the render: %v", err)
	}
	out, err := os.ReadFile(s.MaterializedPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "WARNING: unparseable fact corrupt.md") {
		t.Errorf("expected a warning comment for the corrupt file, got:\n%s", out)
	}
}

func TestEnsureImportAppendsToUnterminatedFile(t *testing.T) {
	dir := t.TempDir()
	ctx := filepath.Join(dir, "CLAUDE.md")
	// Existing content with NO trailing newline — the import must not fuse
	// onto the last line.
	if err := os.WriteFile(ctx, []byte("# My project\nsome notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := EnsureImport(ctx)
	if err != nil || !added {
		t.Fatalf("EnsureImport: added=%v err=%v", added, err)
	}
	got, err := os.ReadFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "some notes"+ImportLine) || strings.Contains(string(got), "notesSee") {
		t.Errorf("import fused onto the unterminated last line:\n%s", got)
	}
	if !strings.Contains(string(got), ImportLine) {
		t.Errorf("import line missing:\n%s", got)
	}
	if !strings.Contains(string(got), "some notes") {
		t.Errorf("original content lost:\n%s", got)
	}
}
