package claudecode

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodeProjectDir(t *testing.T) {
	cases := map[string]string{
		"/Users/a/code/stillroom": "-Users-a-code-stillroom",
		"/tmp/x_y.z":              "-tmp-x-y-z",
		"C:\\work\\repo":          "C--work-repo",
		"/home/dev/项目":            "-home-dev---", // non-ASCII → '-' per byte-class rule
	}
	for in, want := range cases {
		if got := EncodeProjectDir(in); got != want {
			t.Errorf("EncodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDiscoverNewestFirstAndFiltersNonJSONL(t *testing.T) {
	home := t.TempDir()
	cwd := "/repo/demo"
	dir := filepath.Join(home, "projects", EncodeProjectDir(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "old.jsonl")
	newer := filepath.Join(dir, "newer.jsonl")
	for _, p := range []string{old, newer} {
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	sessions, err := Discover(home, cwd)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len = %d, want 2 (txt filtered)", len(sessions))
	}
	if filepath.Base(sessions[0].Path) != "newer.jsonl" {
		t.Fatalf("order wrong: %v", sessions)
	}
}

func TestDiscoverMissingDirIsEmpty(t *testing.T) {
	sessions, err := Discover(t.TempDir(), "/nope")
	if err != nil || sessions != nil {
		t.Fatalf("want empty+nil error, got %v %v", sessions, err)
	}
}
