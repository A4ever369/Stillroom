package ir

import (
	"fmt"
	"strings"
	"time"
)

// Playbook is a reusable recipe for one kind of task, distilled from one or
// more successful sessions. Unlike facts, playbooks are prose documents;
// their fusion is an agentic (LLM-assisted, non-deterministic) revision
// committed as a new version — see docs/design-v2.md §5.
type Playbook struct {
	// ID is the semantic key and file stem, e.g. "customer-onboarding-deploy".
	ID    string
	Title string
	// Sources lists the evidence refs this revision drew from.
	Sources   []string
	UpdatedAt time.Time
	// Body is the recipe: preconditions → steps → known pitfalls (linking
	// fact IDs) → evidence links.
	Body string
}

// Filename is the canonical file name for the playbook inside playbooks/.
func (p *Playbook) Filename() string { return p.ID + ".md" }

// Validate checks the invariants a playbook file must satisfy.
func (p *Playbook) Validate() error {
	if err := ValidateID(p.ID); err != nil {
		return err
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("ir: playbook %s missing title", p.ID)
	}
	if strings.TrimSpace(p.Body) == "" {
		return fmt.Errorf("ir: playbook %s has empty body", p.ID)
	}
	if p.UpdatedAt.IsZero() {
		return fmt.Errorf("ir: playbook %s missing updated_at", p.ID)
	}
	return nil
}

// Encode renders the playbook in its canonical on-disk form (deterministic
// key order, like Fact.Encode).
func (p *Playbook) Encode() []byte {
	var b strings.Builder
	b.WriteString("---\n")
	writeKV(&b, "id", p.ID)
	writeKV(&b, "title", p.Title)
	writeKV(&b, "updated_at", p.UpdatedAt.Format(time.RFC3339))
	if len(p.Sources) > 0 {
		writeKV(&b, "sources", strings.Join(p.Sources, ", "))
	}
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(p.Body))
	b.WriteString("\n")
	return []byte(b.String())
}

// ParsePlaybook parses the canonical playbook file format.
func ParsePlaybook(data []byte) (Playbook, error) {
	fm, body, err := splitFrontmatter(string(data))
	if err != nil {
		return Playbook{}, err
	}
	p := Playbook{Body: strings.TrimSpace(body)}
	for k, v := range fm {
		switch k {
		case "id":
			p.ID = v
		case "title":
			p.Title = v
		case "updated_at":
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return Playbook{}, fmt.Errorf("ir: bad updated_at %q: %w", v, err)
			}
			p.UpdatedAt = t
		case "sources":
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					p.Sources = append(p.Sources, s)
				}
			}
		}
	}
	if err := p.Validate(); err != nil {
		return Playbook{}, err
	}
	return p, nil
}
