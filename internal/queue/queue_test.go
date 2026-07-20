package queue

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnqueueListRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Enqueue(dir, transcript); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// idempotent re-enqueue (resumed sessions re-fire SessionEnd)
	if err := Enqueue(dir, transcript); err != nil {
		t.Fatalf("re-Enqueue: %v", err)
	}
	got, err := List(dir)
	if err != nil || len(got) != 1 || got[0] != transcript {
		t.Fatalf("List = %v, %v", got, err)
	}

	Remove(dir, transcript)
	got, _ = List(dir)
	if len(got) != 0 {
		t.Fatalf("after Remove, List = %v", got)
	}
}

func TestListPrunesDanglingEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	gone := filepath.Join(t.TempDir(), "deleted.jsonl") // never created
	if err := Enqueue(dir, gone); err != nil {
		t.Fatal(err)
	}
	got, err := List(dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("dangling entry should be skipped: %v %v", got, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("dangling entry should be pruned from disk, found %d", len(entries))
	}
}

func TestListMissingDirIsEmpty(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "nope"))
	if err != nil || got != nil {
		t.Fatalf("want empty, got %v %v", got, err)
	}
}
