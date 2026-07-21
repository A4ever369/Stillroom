package queue

// Error and filtering branches (docs/testing.md L1 coverage fill).

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnqueueFailsWhenDirCannotBeCreated(t *testing.T) {
	// A regular file where a queue-dir parent should be: MkdirAll must fail
	// rather than silently write somewhere unexpected.
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Enqueue(filepath.Join(blocker, "queue"), "/some/transcript.jsonl"); err == nil {
		t.Fatal("Enqueue should fail when its dir cannot be created")
	}
}

func TestListSkipsNonPathEntries(t *testing.T) {
	dir := t.TempDir()
	// A real transcript to point at, plus noise the lister must ignore.
	transcript := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(transcript, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Enqueue(dir, transcript); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir.path"), 0o755); err != nil {
		t.Fatal(err) // a directory named like an entry must still be skipped
	}
	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != transcript {
		t.Fatalf("List should return only the real entry, got %v", got)
	}
}
