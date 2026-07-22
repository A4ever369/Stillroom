package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomeHonorsEnv(t *testing.T) {
	t.Setenv("CODEX_HOME", "/custom/codex")
	if got := Home(); got != "/custom/codex" {
		t.Errorf("Home() = %q, want /custom/codex", got)
	}
}

func TestHomeFallsBackToDotCodex(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	got := Home()
	if got == "" || filepath.Base(got) != ".codex" {
		t.Errorf("Home() fallback = %q, want a path ending in .codex", got)
	}
}

func TestDiscoverMatchesByCWD(t *testing.T) {
	home := t.TempDir()
	dayDir := filepath.Join(home, "sessions", "2026", "07", "20")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two rollouts for our repo, one for a different repo, plus a non-rollout.
	write := func(name, cwd string) string {
		p := filepath.Join(dayDir, name)
		line := `{"type":"session_meta","payload":{"session_id":"s","cwd":"` + cwd + `"}}` + "\n"
		if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("rollout-2026-07-20T07-00-00-a.jsonl", "/repo/acme")
	b := write("rollout-2026-07-20T08-00-00-b.jsonl", "/repo/acme")
	write("rollout-2026-07-20T09-00-00-c.jsonl", "/repo/other")
	write("index.json", "/repo/acme") // not a rollout name

	got, err := Discover(home, "/repo/acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rollouts for /repo/acme, got %d: %+v", len(got), got)
	}
	found := map[string]bool{got[0].Path: true, got[1].Path: true}
	if !found[a] || !found[b] {
		t.Errorf("discovery missed one of the matching rollouts: %+v", got)
	}
}

func TestDiscoverMissingSessionsDirIsEmpty(t *testing.T) {
	got, err := Discover(t.TempDir(), "/repo/acme") // no sessions/ subdir
	if err != nil {
		t.Fatalf("missing sessions dir should be empty, not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty discovery, got %+v", got)
	}
}
