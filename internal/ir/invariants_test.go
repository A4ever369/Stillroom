package ir

// Property tests for the two hard rules the knowledge plane rests on
// (CLAUDE.md, docs/testing.md L2):
//
//	determinism  — Encode() and the on-disk layout are byte-stable, so a
//	               repeated run can never produce a git diff.
//	supersession — only moves forward: a newer observed_at replaces, an
//	               older one never clobbers.
//
// Both are checked over randomized inputs rather than a handful of examples,
// because both fail in exactly the corner cases a hand-written table misses.

import (
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// randomFact builds a valid but adversarial fact: multi-byte bodies, blank
// lines, frontmatter-looking text and delimiters that a naive encoder breaks on.
func randomFact(r *rand.Rand, i int) Fact {
	bodies := []string{
		"prod DB entry is pgbouncer on 6432",
		"部署走 make deploy-prod,不要直接 kubectl apply",
		"body with\n\nblank lines and a --- delimiter inside",
		"body: that looks: like: frontmatter\nstatus: active",
		"trailing whitespace   \nand a tab\there",
		"emoji 🚢 and quotes \"double\" and 'single'",
	}
	scopes := []string{"global", "repo:acme-infra", "repo:stillroom"}
	confidences := []Confidence{ConfidenceHigh, ConfidenceMedium, ConfidenceLow}
	statuses := []Status{StatusActive, StatusSuperseded, StatusDisputed}

	return Fact{
		ID:         idFor(i),
		Scope:      scopes[r.Intn(len(scopes))],
		ObservedAt: time.Unix(int64(r.Intn(1_000_000_000)), 0).UTC(),
		Source:     "claude-code://session-" + idFor(r.Intn(50)),
		Confidence: confidences[r.Intn(len(confidences))],
		Status:     statuses[r.Intn(len(statuses))],
		Body:       bodies[r.Intn(len(bodies))],
	}
}

func idFor(i int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	return string(alphabet[i%26]) + "." + string(alphabet[(i/26)%26]) + string(alphabet[i%26])
}

// Encoding must be a pure function: same fact in, same bytes out, always.
func TestEncodeIsDeterministic(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 200; i++ {
		f := randomFact(r, i)
		first := f.Encode()
		for pass := 0; pass < 3; pass++ {
			if got := f.Encode(); string(got) != string(first) {
				t.Fatalf("Encode not stable for %s\npass 0: %q\npass %d: %q", f.ID, first, pass, got)
			}
		}
	}
}

// Encode → ParseFact → Encode must reach a fixed point. Anything else means
// a round trip through disk silently rewrites files and produces git noise.
func TestEncodeParseRoundTripIsStable(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 200; i++ {
		f := randomFact(r, i)
		encoded := f.Encode()
		parsed, err := ParseFact(encoded)
		if err != nil {
			t.Fatalf("re-parsing our own output failed for %s: %v\n%s", f.ID, err, encoded)
		}
		if got := parsed.Encode(); string(got) != string(encoded) {
			t.Fatalf("round trip not a fixed point for %s\nwrote: %q\nreread: %q", f.ID, encoded, got)
		}
	}
}

// The order facts arrive in must not affect what lands on disk — otherwise
// two teammates distilling the same knowledge get different diffs.
func TestWriteOrderDoesNotAffectDisk(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	facts := make([]Fact, 40)
	for i := range facts {
		facts[i] = randomFact(r, i)
	}

	snapshot := func(order []int) map[string]string {
		s := Store{Root: t.TempDir()}
		if err := s.Init(); err != nil {
			t.Fatal(err)
		}
		for _, idx := range order {
			if err := s.WriteFact(facts[idx]); err != nil {
				t.Fatal(err)
			}
		}
		return readDir(t, s.FactsDir())
	}

	forward := make([]int, len(facts))
	backward := make([]int, len(facts))
	shuffled := make([]int, len(facts))
	for i := range facts {
		forward[i] = i
		backward[i] = len(facts) - 1 - i
		shuffled[i] = i
	}
	r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	want := snapshot(forward)
	for name, order := range map[string][]int{"backward": backward, "shuffled": shuffled} {
		got := snapshot(order)
		if len(got) != len(want) {
			t.Fatalf("%s order produced %d files, forward produced %d", name, len(got), len(want))
		}
		for file, body := range want {
			if got[file] != body {
				t.Errorf("%s order changed %s:\n--- forward ---\n%s\n--- %s ---\n%s",
					name, file, body, name, got[file])
			}
		}
	}
}

// Writing the same fact twice must be a no-op on disk, not a rewrite.
func TestRewritingSameFactIsAByteNoOp(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	s := Store{Root: t.TempDir()}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	facts := make([]Fact, 30)
	for i := range facts {
		facts[i] = randomFact(r, i)
		if err := s.WriteFact(facts[i]); err != nil {
			t.Fatal(err)
		}
	}
	before := readDir(t, s.FactsDir())
	for _, f := range facts {
		if err := s.WriteFact(f); err != nil {
			t.Fatal(err)
		}
	}
	after := readDir(t, s.FactsDir())
	for file, body := range before {
		if after[file] != body {
			t.Errorf("rewriting %s changed its bytes — repeated runs would create git diffs\n--- before ---\n%s\n--- after ---\n%s",
				file, body, after[file])
		}
	}
}

// Supersession is monotonic: whatever order observations of one key arrive
// in, the file ends up holding the newest observation and nothing else.
func TestSupersessionOnlyMovesForward(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for trial := 0; trial < 50; trial++ {
		n := 2 + r.Intn(6)
		observations := make([]Fact, n)
		for i := range observations {
			observations[i] = Fact{
				ID:         "deploy.acme.db-endpoint",
				Scope:      "global",
				ObservedAt: time.Unix(int64(r.Intn(100_000)), 0).UTC(),
				Source:     "claude-code://s",
				Confidence: ConfidenceHigh,
				Status:     StatusActive,
				Body:       "observation " + time.Unix(int64(i), 0).UTC().String(),
			}
		}
		newest := observations[0]
		for _, o := range observations {
			if o.ObservedAt.After(newest.ObservedAt) {
				newest = o
			}
		}

		s := Store{Root: t.TempDir()}
		if err := s.Init(); err != nil {
			t.Fatal(err)
		}
		for _, o := range observations {
			if err := s.WriteFact(o); err != nil {
				t.Fatal(err)
			}
		}

		facts, bad, err := s.LoadFacts()
		if err != nil || len(bad) != 0 {
			t.Fatalf("trial %d: load failed: %v %v", trial, err, bad)
		}
		if len(facts) != 1 {
			t.Fatalf("trial %d: want one file for one key, got %d", trial, len(facts))
		}
		if !facts[0].ObservedAt.Equal(newest.ObservedAt) {
			t.Errorf("trial %d: stored observed_at = %s, want the newest %s — an older observation clobbered a newer one",
				trial, facts[0].ObservedAt, newest.ObservedAt)
		}
	}
}

// An older observation arriving last must leave the newer one untouched,
// byte for byte — not merely keep its timestamp.
func TestOlderObservationDoesNotTouchTheFile(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	base := Fact{
		ID: "a.b", Scope: "global", Source: "s", Confidence: ConfidenceHigh, Status: StatusActive,
		ObservedAt: time.Unix(2000, 0).UTC(), Body: "the new truth",
	}
	if err := s.WriteFact(base); err != nil {
		t.Fatal(err)
	}
	before := readDir(t, s.FactsDir())

	stale := base
	stale.ObservedAt = time.Unix(1000, 0).UTC()
	stale.Body = "the stale truth"
	if err := s.WriteFact(stale); err != nil {
		t.Fatal(err)
	}

	after := readDir(t, s.FactsDir())
	for file, body := range before {
		if after[file] != body {
			t.Errorf("a stale observation rewrote %s\n--- before ---\n%s\n--- after ---\n%s", file, body, after[file])
		}
	}
}

func readDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make(map[string]string, len(names))
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		out[n] = string(data)
	}
	return out
}

// Regression: observed_at is stored at second precision, so a timestamp
// carrying nanoseconds (a file mtime, say) used to compare as strictly newer
// than its own re-parsed self — forging a supersedes entry on every rewrite.
func TestSubSecondTimestampsDoNotForgeSupersession(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	f := Fact{
		ID: "a.b", Scope: "global", Source: "s",
		Confidence: ConfidenceHigh, Status: StatusActive,
		ObservedAt: time.Unix(1700000000, 123456789).UTC(),
		Body:       "same observation, written twice",
	}
	if err := s.WriteFact(f); err != nil {
		t.Fatal(err)
	}
	before := readDir(t, s.FactsDir())
	if err := s.WriteFact(f); err != nil {
		t.Fatal(err)
	}
	for name, body := range readDir(t, s.FactsDir()) {
		if body != before[name] {
			t.Errorf("rewriting an identical observation changed %s:\n--- before ---\n%s\n--- after ---\n%s",
				name, before[name], body)
		}
		if strings.Contains(body, "supersedes:") {
			t.Errorf("an identical observation forged a supersedes entry:\n%s", body)
		}
	}
}
