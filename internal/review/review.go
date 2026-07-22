// Package review renders a human-readable summary of how a PR changes the
// knowledge plane. It is the last piece of the "review parasite" strategy
// (docs/design-v2.md §13): knowledge review rides the team's normal PR review
// instead of needing a bespoke surface — a bot comments the fact/playbook diff
// in plain language so a reviewer skims it alongside the code.
//
// The diff is semantic, not textual: because one fact is one file keyed by a
// stable ID, added/updated/removed is computed by ID, and a fact whose
// observation advanced is flagged as a supersession rather than a raw edit.
package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/0xbeekeeper/stillroom/internal/ir"
)

// Snapshot is the knowledge plane at one git ref.
type Snapshot struct {
	Facts     []ir.Fact
	Playbooks []ir.Playbook
}

// FactChange is one fact that exists in both snapshots with a changed encoding.
type FactChange struct {
	Before ir.Fact
	After  ir.Fact
}

// Superseded reports whether the change advanced the observation (the newer
// side carries a supersedes pointer or the observed_at moved forward).
func (c FactChange) Superseded() bool {
	return c.After.Supersedes != "" && c.After.Supersedes != c.Before.Supersedes ||
		c.After.ObservedAt.After(c.Before.ObservedAt)
}

// Summary is the classified difference between two snapshots.
type Summary struct {
	NewFacts     []ir.Fact
	UpdatedFacts []FactChange
	RemovedFacts []ir.Fact

	NewPlaybooks     []ir.Playbook
	UpdatedPlaybooks []ir.Playbook
	RemovedPlaybooks []ir.Playbook
}

// Empty reports whether nothing about the knowledge plane changed.
func (s Summary) Empty() bool {
	return len(s.NewFacts)+len(s.UpdatedFacts)+len(s.RemovedFacts)+
		len(s.NewPlaybooks)+len(s.UpdatedPlaybooks)+len(s.RemovedPlaybooks) == 0
}

// Diff classifies head against base by ID. Encoding equality (byte-stable by
// the determinism invariant) is the change signal, so a no-op rewrite produces
// no diff.
func Diff(base, head Snapshot) Summary {
	var s Summary

	baseFacts := indexFacts(base.Facts)
	headFacts := indexFacts(head.Facts)
	for _, f := range sortedFactKeys(headFacts) {
		hf := headFacts[f]
		bf, ok := baseFacts[f]
		if !ok {
			s.NewFacts = append(s.NewFacts, hf)
			continue
		}
		if !bytesEqual(bf.Encode(), hf.Encode()) {
			s.UpdatedFacts = append(s.UpdatedFacts, FactChange{Before: bf, After: hf})
		}
	}
	for _, f := range sortedFactKeys(baseFacts) {
		if _, ok := headFacts[f]; !ok {
			s.RemovedFacts = append(s.RemovedFacts, baseFacts[f])
		}
	}

	basePbs := indexPlaybooks(base.Playbooks)
	headPbs := indexPlaybooks(head.Playbooks)
	for _, id := range sortedPlaybookKeys(headPbs) {
		hp := headPbs[id]
		bp, ok := basePbs[id]
		if !ok {
			s.NewPlaybooks = append(s.NewPlaybooks, hp)
			continue
		}
		if !bytesEqual(bp.Encode(), hp.Encode()) {
			s.UpdatedPlaybooks = append(s.UpdatedPlaybooks, hp)
		}
	}
	for _, id := range sortedPlaybookKeys(basePbs) {
		if _, ok := headPbs[id]; !ok {
			s.RemovedPlaybooks = append(s.RemovedPlaybooks, basePbs[id])
		}
	}
	return s
}

// Marker is an invisible anchor at the top of every rendered comment so a
// workflow can find and update its own comment in place instead of posting a
// new one each push.
const Marker = "<!-- stillroom-knowledge-diff -->"

// Markdown renders the summary as a PR comment. It is deterministic (sorted
// throughout) so re-running on the same diff produces an identical comment,
// which lets a workflow update its comment in place without churn.
func (s Summary) Markdown() string {
	if s.Empty() {
		return Marker + "\n### 🧠 Team knowledge changes\n\n_No fact or playbook changes in this PR._\n"
	}
	var b strings.Builder
	b.WriteString(Marker + "\n")
	b.WriteString("### 🧠 Team knowledge changes\n\n")
	fmt.Fprintf(&b, "**Facts:** ➕ %d new · ✏️ %d updated · ➖ %d removed  \n",
		len(s.NewFacts), len(s.UpdatedFacts), len(s.RemovedFacts))
	fmt.Fprintf(&b, "**Playbooks:** ➕ %d new · ✏️ %d updated · ➖ %d removed\n",
		len(s.NewPlaybooks), len(s.UpdatedPlaybooks), len(s.RemovedPlaybooks))

	if len(s.NewFacts) > 0 {
		b.WriteString("\n#### ➕ New facts\n")
		for _, f := range s.NewFacts {
			fmt.Fprintf(&b, "- **`%s`** _(%s)_: %s\n", f.ID, f.Confidence, oneLine(f.Body))
		}
	}
	if len(s.UpdatedFacts) > 0 {
		b.WriteString("\n#### ✏️ Updated facts\n")
		for _, c := range s.UpdatedFacts {
			fmt.Fprintf(&b, "- **`%s`**: %s\n", c.After.ID, oneLine(c.After.Body))
			if c.Superseded() {
				fmt.Fprintf(&b, "  - ♻️ supersedes an earlier observation (%s → %s)\n",
					c.Before.ObservedAt.Format("2006-01-02"), c.After.ObservedAt.Format("2006-01-02"))
			}
		}
	}
	if len(s.RemovedFacts) > 0 {
		b.WriteString("\n#### ➖ Removed facts\n")
		for _, f := range s.RemovedFacts {
			fmt.Fprintf(&b, "- **`%s`**: %s\n", f.ID, oneLine(f.Body))
		}
	}

	if len(s.NewPlaybooks)+len(s.UpdatedPlaybooks)+len(s.RemovedPlaybooks) > 0 {
		b.WriteString("\n#### 📗 Playbooks\n")
		for _, p := range s.NewPlaybooks {
			fmt.Fprintf(&b, "- ➕ **%s** — %s\n", p.ID, p.Title)
		}
		for _, p := range s.UpdatedPlaybooks {
			fmt.Fprintf(&b, "- ✏️ **%s** — %s\n", p.ID, p.Title)
		}
		for _, p := range s.RemovedPlaybooks {
			fmt.Fprintf(&b, "- ➖ **%s** — %s\n", p.ID, p.Title)
		}
	}

	b.WriteString("\n<sub>Generated by `still review` — review these as you would code.</sub>\n")
	return b.String()
}

// oneLine flattens and clips a body so the comment stays skimmable.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 200
	if len(s) <= max {
		return s
	}
	// Cut on a rune boundary.
	i := max
	for i > 0 && !isRuneStart(s[i]) {
		i--
	}
	return s[:i] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func indexFacts(facts []ir.Fact) map[string]ir.Fact {
	m := make(map[string]ir.Fact, len(facts))
	for _, f := range facts {
		m[f.ID] = f
	}
	return m
}

func indexPlaybooks(pbs []ir.Playbook) map[string]ir.Playbook {
	m := make(map[string]ir.Playbook, len(pbs))
	for _, p := range pbs {
		m[p.ID] = p
	}
	return m
}

func sortedFactKeys(m map[string]ir.Fact) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedPlaybookKeys(m map[string]ir.Playbook) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func bytesEqual(a, b []byte) bool { return string(a) == string(b) }
