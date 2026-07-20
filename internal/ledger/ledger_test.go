package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeenAfterMark(t *testing.T) {
	l := Open(filepath.Join(t.TempDir(), ".local"))
	if l.Seen("/a.jsonl") {
		t.Fatal("empty ledger should not have seen anything")
	}
	if err := l.Mark(Entry{Transcript: "/a.jsonl", SessionID: "s1", Facts: 3}); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if err := l.Mark(Entry{Transcript: "/b.jsonl", Facts: 0}); err != nil {
		t.Fatalf("Mark 2: %v", err)
	}
	if !l.Seen("/a.jsonl") || !l.Seen("/b.jsonl") {
		t.Fatal("marked transcripts should be seen")
	}
	if l.Seen("/c.jsonl") {
		t.Fatal("unmarked transcript should not be seen")
	}
}

func TestSeenSurvivesCorruptLine(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".local")
	l := Open(dir)
	if err := l.Mark(Entry{Transcript: "/a.jsonl"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "distilled.jsonl"), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{corrupt\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := l.Mark(Entry{Transcript: "/b.jsonl"}); err != nil {
		t.Fatal(err)
	}
	if !l.Seen("/a.jsonl") || !l.Seen("/b.jsonl") {
		t.Fatal("corrupt middle line must not break the ledger")
	}
}
