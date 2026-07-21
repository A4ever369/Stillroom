package distill

// Covers the distill paths ir_test/distill_test leave open (docs/testing.md L1):
// BuildPrompt's context-injection branches, Run's error/Now-default behavior,
// and Apply's failure path.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xbeekeeper/stillroom/internal/adapter/claudecode"
	"github.com/0xbeekeeper/stillroom/internal/ir"
)

func TestBuildPromptInjectsExistingContext(t *testing.T) {
	opts := Options{
		Scope:             "repo:x",
		ExistingFactIDs:   []string{"deploy.acme.db-endpoint"},
		ExistingPlaybooks: []string{"acme-deploy — Deploy Acme"},
	}
	p := BuildPrompt("the digest text", opts)
	for _, want := range []string{
		"deploy.acme.db-endpoint", // steer toward reusing fact keys
		"acme-deploy — Deploy Acme",
		"REUSE",           // the reuse instruction for facts
		"the digest text", // the digest is embedded
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildPromptOmitsInjectionWhenEmpty(t *testing.T) {
	p := BuildPrompt("d", Options{Scope: "repo:x"})
	// With no existing knowledge, neither injection header should appear —
	// otherwise the model sees dangling "already has these ids:" with nothing.
	if strings.Contains(p, "already has these fact ids") {
		t.Error("fact-id injection header leaked with no ids")
	}
	if strings.Contains(p, "Existing playbooks") {
		t.Error("playbook injection header leaked with no playbooks")
	}
}

func TestRunDefaultsNowWhenZero(t *testing.T) {
	fake := func(ctx context.Context, prompt string) (string, error) {
		return `{"facts":[{"id":"a.b","confidence":"low","body":"x"}]}`, nil
	}
	// opts.Now left zero on purpose: Run must stamp a real observation time,
	// or the resulting fact fails Validate downstream.
	prop, err := Run(context.Background(), fake, claudecode.Digest{Text: "x"}, Options{Scope: "repo:x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prop.Facts) != 1 || prop.Facts[0].ObservedAt.IsZero() {
		t.Fatalf("Run left observed_at zero: %+v", prop.Facts)
	}
}

func TestRunPropagatesRunnerError(t *testing.T) {
	sentinel := errors.New("claude exploded")
	fake := func(ctx context.Context, prompt string) (string, error) {
		return "", sentinel
	}
	_, err := Run(context.Background(), fake, claudecode.Digest{Text: "x"}, Options{Scope: "repo:x"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("runner error should surface unchanged, got %v", err)
	}
}

func TestApplyStopsAndReportsOnWriteFailure(t *testing.T) {
	// Make the store root's .team-context a regular FILE so MkdirAll of
	// facts/ inside WriteFact fails — a stand-in for any I/O failure.
	root := t.TempDir()
	if err := os.WriteFile(root+"/"+ir.DirName, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := ir.Store{Root: root}
	prop := Proposal{Facts: []ir.Fact{{
		ID: "a.b", Scope: "repo:x", ObservedAt: time.Unix(1700000000, 0).UTC(), Source: "s",
		Confidence: ir.ConfidenceHigh, Status: ir.StatusActive, Body: "b",
	}}}
	written, err := Apply(s, prop)
	if err == nil {
		t.Fatal("Apply should fail when the store cannot be written")
	}
	if len(written) != 0 {
		t.Errorf("nothing should be reported written on failure, got %v", written)
	}
}
