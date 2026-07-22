package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/A4ever369/Stillroom/internal/ir"
)

func seededStore(t *testing.T) ir.Store {
	t.Helper()
	s := ir.Store{Root: t.TempDir()}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	facts := []ir.Fact{
		{ID: "deploy.acme.db-endpoint", Scope: "repo:acme", ObservedAt: now, Source: "claude-code://a",
			Confidence: ir.ConfidenceHigh, Status: ir.StatusActive, Body: "入口是 pgbouncer 6432。"},
		{ID: "old.retired.fact", Scope: "repo:acme", ObservedAt: now, Source: "claude-code://b",
			Confidence: ir.ConfidenceHigh, Status: ir.StatusSuperseded, Body: "已过期的事实。"},
		{ID: "shaky.guess", Scope: "global", ObservedAt: now, Source: "claude-code://c",
			Confidence: ir.ConfidenceLow, Status: ir.StatusActive, Body: "低置信度猜测。"},
	}
	for _, f := range facts {
		if err := s.WriteFact(f); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.WritePlaybook(ir.Playbook{
		ID: "customer-onboarding-deploy", Title: "客户上线部署",
		UpdatedAt: now, Body: "## 步骤\n...",
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunRendersActiveOnly(t *testing.T) {
	s := seededStore(t)
	if _, err := Run(s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(s.MaterializedPath())
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "deploy.acme.db-endpoint") {
		t.Error("active fact missing")
	}
	if strings.Contains(out, "old.retired.fact") {
		t.Error("superseded fact must not be materialized")
	}
	if !strings.Contains(out, "confidence: low") {
		t.Error("low confidence should be visibly flagged")
	}
	if !strings.Contains(out, "customer-onboarding-deploy") {
		t.Error("playbook index missing")
	}
}

func TestRunIsDeterministic(t *testing.T) {
	s := seededStore(t)
	if _, err := Run(s); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(s.MaterializedPath())
	if _, err := Run(s); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(s.MaterializedPath())
	if string(first) != string(second) {
		t.Fatal("repeated materialization produced git noise")
	}
}

func TestEnsureImportIdempotent(t *testing.T) {
	dir := t.TempDir()
	claudeMd := filepath.Join(dir, "CLAUDE.md")

	added, err := EnsureImport(claudeMd)
	if err != nil || !added {
		t.Fatalf("first EnsureImport: added=%v err=%v", added, err)
	}
	added, err = EnsureImport(claudeMd)
	if err != nil || added {
		t.Fatalf("second EnsureImport should be a no-op: added=%v err=%v", added, err)
	}
	data, _ := os.ReadFile(claudeMd)
	if got := strings.Count(string(data), ImportLine); got != 1 {
		t.Fatalf("import line count = %d, want 1", got)
	}
}

func TestEnsureImportPreservesExistingContent(t *testing.T) {
	dir := t.TempDir()
	claudeMd := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMd, []byte("# My Project\n\nrules here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureImport(claudeMd); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(claudeMd)
	if !strings.Contains(string(data), "# My Project") {
		t.Fatal("existing content was destroyed")
	}
}
