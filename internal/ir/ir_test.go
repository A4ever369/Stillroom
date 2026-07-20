package ir

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleFact() Fact {
	return Fact{
		ID:         "deploy.acme.db-endpoint",
		Scope:      "repo:acme-infra",
		ObservedAt: time.Date(2026, 7, 18, 9, 30, 0, 0, time.UTC),
		Source:     "claude-code://a3f9c2",
		Confidence: ConfidenceHigh,
		Status:     StatusActive,
		Body:       "Acme 生产库入口是 pgbouncer(6432),直连 5432 会被安全组拦。",
	}
}

func TestFactEncodeParseRoundTrip(t *testing.T) {
	f := sampleFact()
	got, err := ParseFact(f.Encode())
	if err != nil {
		t.Fatalf("ParseFact: %v", err)
	}
	if got.ID != f.ID || !got.ObservedAt.Equal(f.ObservedAt) || got.Body != f.Body ||
		got.Confidence != f.Confidence || got.Status != f.Status || got.Scope != f.Scope {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, f)
	}
}

func TestFactEncodeDeterministic(t *testing.T) {
	f := sampleFact()
	if string(f.Encode()) != string(f.Encode()) {
		t.Fatal("Encode is not deterministic")
	}
}

func TestParseFactRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"no fence":   "id: x\n",
		"bad status": "---\nid: a.b\nobserved_at: 2026-07-18T09:30:00Z\nconfidence: high\nstatus: nonsense\n---\nbody\n",
		"empty body": "---\nid: a.b\nobserved_at: 2026-07-18T09:30:00Z\nconfidence: high\nstatus: active\n---\n\n",
		"bad id":     "---\nid: Not A Slug\nobserved_at: 2026-07-18T09:30:00Z\nconfidence: high\nstatus: active\n---\nbody\n",
	}
	for name, raw := range cases {
		if _, err := ParseFact([]byte(raw)); err == nil {
			t.Errorf("%s: expected error, got none", name)
		}
	}
}

func TestParseFactToleratesUnknownKeys(t *testing.T) {
	raw := "---\nid: a.b\nobserved_at: 2026-07-18T09:30:00Z\nconfidence: high\nstatus: active\nfuture_key: whatever\n---\nbody\n"
	if _, err := ParseFact([]byte(raw)); err != nil {
		t.Fatalf("unknown frontmatter key should be tolerated: %v", err)
	}
}

func TestStoreWriteFactSupersession(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	old := sampleFact()
	if err := s.WriteFact(old); err != nil {
		t.Fatalf("write old: %v", err)
	}

	// A NEWER observation replaces and records supersession.
	newer := old
	newer.ObservedAt = old.ObservedAt.Add(48 * time.Hour)
	newer.Body = "入口迁移到了 pgcat,端口不变。"
	if err := s.WriteFact(newer); err != nil {
		t.Fatalf("write newer: %v", err)
	}
	facts, bad, err := s.LoadFacts()
	if err != nil || bad != nil {
		t.Fatalf("LoadFacts: %v %v", err, bad)
	}
	if len(facts) != 1 || facts[0].Body != newer.Body {
		t.Fatalf("newer observation should win: %+v", facts)
	}
	if facts[0].Supersedes == "" {
		t.Fatal("supersedes pointer should be auto-filled")
	}

	// An OLDER observation must NOT clobber the newer one.
	stale := old
	stale.Body = "旧观察不应覆盖"
	if err := s.WriteFact(stale); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	facts, _, _ = s.LoadFacts()
	if facts[0].Body != newer.Body {
		t.Fatal("stale observation clobbered a newer fact")
	}
}

func TestStoreLoadFactsIsolatesBadFiles(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	good := sampleFact()
	if err := s.WriteFact(good); err != nil {
		t.Fatalf("WriteFact: %v", err)
	}
	badPath := filepath.Join(s.FactsDir(), "broken.md")
	if err := os.WriteFile(badPath, []byte("not a fact"), 0o644); err != nil {
		t.Fatal(err)
	}
	facts, bad, err := s.LoadFacts()
	if err != nil {
		t.Fatalf("LoadFacts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("good fact should load, got %d", len(facts))
	}
	if bad == nil || bad["broken.md"] == nil {
		t.Fatal("broken file should be reported, not swallowed")
	}
}

func TestPlaybookRoundTrip(t *testing.T) {
	p := Playbook{
		ID:        "customer-onboarding-deploy",
		Title:     "客户上线部署",
		Sources:   []string{"claude-code://a3f9c2", "claude-code://b7d1e0"},
		UpdatedAt: time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC),
		Body:      "## 前提\n...\n## 步骤\n...",
	}
	got, err := ParsePlaybook(p.Encode())
	if err != nil {
		t.Fatalf("ParsePlaybook: %v", err)
	}
	if got.ID != p.ID || got.Title != p.Title || len(got.Sources) != 2 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}
