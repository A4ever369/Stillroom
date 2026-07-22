package distill

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/0xbeekeeper/stillroom/internal/ir"
	"github.com/0xbeekeeper/stillroom/internal/session"
)

func fixedOpts() Options {
	return Options{
		Scope:           "repo:traces-git",
		SourceRef:       "claude-code://test-session",
		ExistingFactIDs: []string{"deploy.acme.db-endpoint"},
		Now:             time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC),
	}
}

func TestRunParsesWellFormedProposal(t *testing.T) {
	fake := func(ctx context.Context, prompt string) (string, error) {
		if !strings.Contains(prompt, "deploy.acme.db-endpoint") {
			t.Error("existing fact ids should be in the prompt")
		}
		return `{"facts":[{"id":"ci.postgres.image","confidence":"high","body":"CI 用 pgvector/pgvector:pg17 镜像。"}],"playbook":{"id":"release-flow","title":"发布流程","body":"步骤..."}}`, nil
	}
	prop, err := Run(context.Background(), fake, session.Digest{Text: "[user] hi"}, fixedOpts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prop.Facts) != 1 || prop.Facts[0].ID != "ci.postgres.image" {
		t.Fatalf("facts = %+v", prop.Facts)
	}
	f := prop.Facts[0]
	if f.Scope != "repo:traces-git" || f.Source != "claude-code://test-session" ||
		f.Status != ir.StatusActive || !f.ObservedAt.Equal(fixedOpts().Now) {
		t.Fatalf("fact not stamped with options: %+v", f)
	}
	if prop.Playbook == nil || prop.Playbook.ID != "release-flow" {
		t.Fatalf("playbook = %+v", prop.Playbook)
	}
}

func TestRunSurvivesMarkdownFenceAndDropsBadItems(t *testing.T) {
	fake := func(ctx context.Context, prompt string) (string, error) {
		return "```json\n{\"facts\":[{\"id\":\"BAD ID!\",\"body\":\"x\"},{\"id\":\"ok.fact\",\"confidence\":\"low\",\"body\":\"有效条目\"}],\"playbook\":null}\n```", nil
	}
	prop, err := Run(context.Background(), fake, session.Digest{Text: "x"}, fixedOpts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prop.Facts) != 1 || prop.Facts[0].ID != "ok.fact" {
		t.Fatalf("bad item should be dropped, good kept: %+v", prop.Facts)
	}
}

func TestRunRedactsProposalOutput(t *testing.T) {
	fake := func(ctx context.Context, prompt string) (string, error) {
		return `{"facts":[{"id":"vault.token.location","confidence":"high","body":"token 是 ghp_16C7e42F292c6912E7710c838347Ae178B4a"}],"playbook":null}`, nil
	}
	prop, err := Run(context.Background(), fake, session.Digest{Text: "x"}, fixedOpts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(prop.Facts[0].Body, "ghp_") {
		t.Fatalf("secret survived distillation: %q", prop.Facts[0].Body)
	}
	if prop.Redactions == 0 {
		t.Fatal("redaction count should be reported")
	}
}

func TestRunRejectsNonJSON(t *testing.T) {
	fake := func(ctx context.Context, prompt string) (string, error) {
		return "I could not find anything interesting.", nil
	}
	if _, err := Run(context.Background(), fake, session.Digest{Text: "x"}, fixedOpts()); err == nil {
		t.Fatal("expected error for non-JSON output")
	}
}

func TestApplyWritesFilesAndReportsPaths(t *testing.T) {
	s := ir.Store{Root: t.TempDir()}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	prop := Proposal{
		Facts: []ir.Fact{{
			ID: "a.b", Scope: "repo:x", ObservedAt: now, Source: "s",
			Confidence: ir.ConfidenceHigh, Status: ir.StatusActive, Body: "b",
		}},
		Playbook: &ir.Playbook{ID: "p.b", Title: "t", UpdatedAt: now, Body: "steps"},
	}
	written, err := Apply(s, prop)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("written = %v", written)
	}
	facts, bad, err := s.LoadFacts()
	if err != nil || bad != nil || len(facts) != 1 {
		t.Fatalf("reload: %v %v %v", facts, bad, err)
	}
}
