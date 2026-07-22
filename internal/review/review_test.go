package review

import (
	"strings"
	"testing"
	"time"

	"github.com/A4ever369/Stillroom/internal/ir"
)

func fact(id, body string, observed time.Time) ir.Fact {
	return ir.Fact{
		ID: id, Scope: "repo:x", Source: "claude-code://s",
		ObservedAt: observed, Confidence: ir.ConfidenceHigh, Status: ir.StatusActive,
		Body: body,
	}
}

func TestDiffClassifiesFacts(t *testing.T) {
	t0 := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)

	base := Snapshot{Facts: []ir.Fact{
		fact("keep.same", "unchanged", t0),
		fact("change.me", "old body", t0),
		fact("remove.me", "going away", t0),
	}}
	updated := fact("change.me", "new body", t1)
	updated.Supersedes = "change.me@2026-07-20"
	head := Snapshot{Facts: []ir.Fact{
		fact("keep.same", "unchanged", t0),
		updated,
		fact("brand.new", "fresh knowledge", t1),
	}}

	s := Diff(base, head)
	if len(s.NewFacts) != 1 || s.NewFacts[0].ID != "brand.new" {
		t.Errorf("new = %+v", s.NewFacts)
	}
	if len(s.UpdatedFacts) != 1 || s.UpdatedFacts[0].After.ID != "change.me" {
		t.Errorf("updated = %+v", s.UpdatedFacts)
	}
	if !s.UpdatedFacts[0].Superseded() {
		t.Error("advanced observation with a supersedes pointer should flag as superseded")
	}
	if len(s.RemovedFacts) != 1 || s.RemovedFacts[0].ID != "remove.me" {
		t.Errorf("removed = %+v", s.RemovedFacts)
	}
}

func TestDiffIgnoresIdenticalRewrite(t *testing.T) {
	t0 := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	snap := Snapshot{Facts: []ir.Fact{fact("a.b", "same", t0)}}
	// Same bytes on both sides — the determinism invariant means no diff.
	if s := Diff(snap, snap); !s.Empty() {
		t.Errorf("identical snapshots should produce an empty diff, got %+v", s)
	}
}

func TestDiffClassifiesPlaybooks(t *testing.T) {
	t0 := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	pb := func(id, title, body string) ir.Playbook {
		return ir.Playbook{ID: id, Title: title, UpdatedAt: t0, Body: body}
	}
	base := Snapshot{Playbooks: []ir.Playbook{pb("keep", "Keep", "## a"), pb("edit", "Edit", "## old")}}
	head := Snapshot{Playbooks: []ir.Playbook{pb("keep", "Keep", "## a"), pb("edit", "Edit", "## new"), pb("new", "New", "## x")}}

	s := Diff(base, head)
	if len(s.NewPlaybooks) != 1 || s.NewPlaybooks[0].ID != "new" {
		t.Errorf("new playbooks = %+v", s.NewPlaybooks)
	}
	if len(s.UpdatedPlaybooks) != 1 || s.UpdatedPlaybooks[0].ID != "edit" {
		t.Errorf("updated playbooks = %+v", s.UpdatedPlaybooks)
	}
}

func TestMarkdownEmptyIsExplicit(t *testing.T) {
	got := Summary{}.Markdown()
	if !strings.Contains(got, "No fact or playbook changes") {
		t.Errorf("empty summary should say so plainly:\n%s", got)
	}
}

func TestMarkdownIsDeterministic(t *testing.T) {
	t0 := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	// Feed facts out of ID order; the render must sort so a workflow can update
	// its comment in place without spurious churn.
	head := Snapshot{Facts: []ir.Fact{
		fact("z.last", "z body", t0),
		fact("a.first", "a body", t0),
	}}
	s := Diff(Snapshot{}, head)
	first := s.Markdown()
	if a, b := strings.Index(first, "a.first"), strings.Index(first, "z.last"); a < 0 || b < 0 || a > b {
		t.Errorf("facts not sorted in output:\n%s", first)
	}
	if second := Diff(Snapshot{}, head).Markdown(); second != first {
		t.Error("Markdown is not deterministic across runs")
	}
}

func TestMarkdownRendersEverySection(t *testing.T) {
	t0 := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	pb := func(id, title string) ir.Playbook {
		return ir.Playbook{ID: id, Title: title, UpdatedAt: t0, Body: "## x"}
	}
	base := Snapshot{
		Facts:     []ir.Fact{fact("change.me", "old", t0), fact("remove.me", "gone", t0)},
		Playbooks: []ir.Playbook{pb("edit", "Edit"), pb("drop", "Drop")},
	}
	newer := fact("change.me", "new", t1)
	head := Snapshot{
		Facts:     []ir.Fact{newer, fact("brand.new", "fresh", t1)},
		Playbooks: []ir.Playbook{{ID: "edit", Title: "Edit", UpdatedAt: t1, Body: "## y"}, pb("added", "Added")},
	}
	md := Diff(base, head).Markdown()
	for _, want := range []string{
		"➕ New facts", "brand.new",
		"✏️ Updated facts", "change.me", "♻️ supersedes",
		"➖ Removed facts", "remove.me",
		"📗 Playbooks", "➕ **added**", "✏️ **edit**", "➖ **drop**",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestOneLineFlattensAndClips(t *testing.T) {
	if got := oneLine("a\n  b   c"); got != "a b c" {
		t.Errorf("oneLine = %q", got)
	}
	long := oneLine(strings.Repeat("x", 500))
	if len([]rune(long)) > 201 { // 200 + ellipsis
		t.Errorf("oneLine did not clip: %d runes", len([]rune(long)))
	}
	// A multi-byte body clipped at the boundary must stay valid UTF-8.
	cn := oneLine(strings.Repeat("中", 500))
	for _, r := range cn {
		if r == '�' {
			t.Fatal("oneLine split a rune")
		}
	}
}
