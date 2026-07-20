// Package ir holds the knowledge-plane intermediate representation of
// Traces Git: facts and playbooks distilled from human×AI sessions.
//
// Design rule (docs/design-v2.md §1): fusion happens ONLY in this layer.
// Raw transcripts are evidence — append-only, never merged, only referenced
// via the Source field. One fact = one file so that git's directory merge is
// the fusion algorithm (§2).
package ir

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Confidence expresses how sure the distiller (or a human editor) is.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Status carries the temporal-validity semantics of a fact (§3.1):
// only active facts are materialized; superseded facts stay for lineage;
// disputed facts wait for human adjudication.
type Status string

const (
	StatusActive     Status = "active"
	StatusSuperseded Status = "superseded"
	StatusDisputed   Status = "disputed"
)

// Fact is one distilled, independently-injectable piece of knowledge.
type Fact struct {
	// ID is the semantic key (fact identity) and the file stem,
	// e.g. "deploy.acme.db-endpoint".
	ID string
	// Scope narrows where the fact applies, e.g. "repo:acme-infra" or "global".
	Scope string
	// ObservedAt is when the fact was learned. Newer observations of the
	// same key from the same lineage supersede older ones.
	ObservedAt time.Time
	// Source points back to the evidence plane,
	// e.g. "trace://allen/a3f9c2/turns/41-58" or a transcript path ref.
	Source     string
	Confidence Confidence
	Status     Status
	// Supersedes optionally names the observation this one replaces,
	// e.g. "deploy.acme.db-endpoint@2026-05-02".
	Supersedes string
	// Body is the fact statement itself: natural language, one thing per fact.
	Body string
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*[a-z0-9]$`)

// ValidateID reports whether s is a legal fact/playbook semantic key.
func ValidateID(s string) error {
	if len(s) < 2 || len(s) > 200 {
		return fmt.Errorf("ir: id %q must be 2-200 chars", s)
	}
	if !idPattern.MatchString(s) {
		return fmt.Errorf("ir: id %q must match %s", s, idPattern)
	}
	return nil
}

// Validate checks the invariants a fact file must satisfy before it is
// written into the knowledge repo.
func (f *Fact) Validate() error {
	if err := ValidateID(f.ID); err != nil {
		return err
	}
	if strings.TrimSpace(f.Body) == "" {
		return fmt.Errorf("ir: fact %s has empty body", f.ID)
	}
	if f.ObservedAt.IsZero() {
		return fmt.Errorf("ir: fact %s missing observed_at", f.ID)
	}
	switch f.Status {
	case StatusActive, StatusSuperseded, StatusDisputed:
	default:
		return fmt.Errorf("ir: fact %s has invalid status %q", f.ID, f.Status)
	}
	switch f.Confidence {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
	default:
		return fmt.Errorf("ir: fact %s has invalid confidence %q", f.ID, f.Confidence)
	}
	return nil
}

// Filename is the canonical file name for the fact inside facts/.
func (f *Fact) Filename() string { return f.ID + ".md" }

// Encode renders the fact in its canonical on-disk form. Encoding is
// deterministic (fixed key order) so that identical facts produce identical
// bytes — a prerequisite for git-level dedup.
func (f *Fact) Encode() []byte {
	var b strings.Builder
	b.WriteString("---\n")
	writeKV(&b, "id", f.ID)
	writeKV(&b, "scope", f.Scope)
	writeKV(&b, "observed_at", f.ObservedAt.Format(time.RFC3339))
	writeKV(&b, "source", f.Source)
	writeKV(&b, "confidence", string(f.Confidence))
	writeKV(&b, "status", string(f.Status))
	if f.Supersedes != "" {
		writeKV(&b, "supersedes", f.Supersedes)
	}
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(f.Body))
	b.WriteString("\n")
	return []byte(b.String())
}

func writeKV(b *strings.Builder, k, v string) {
	fmt.Fprintf(b, "%s: %s\n", k, v)
}

// ParseFact parses the canonical fact file format. It is tolerant of unknown
// frontmatter keys (forward compatibility) but strict about the invariants
// checked by Validate.
func ParseFact(data []byte) (Fact, error) {
	fm, body, err := splitFrontmatter(string(data))
	if err != nil {
		return Fact{}, err
	}
	f := Fact{Body: strings.TrimSpace(body)}
	for k, v := range fm {
		switch k {
		case "id":
			f.ID = v
		case "scope":
			f.Scope = v
		case "observed_at":
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return Fact{}, fmt.Errorf("ir: bad observed_at %q: %w", v, err)
			}
			f.ObservedAt = t
		case "source":
			f.Source = v
		case "confidence":
			f.Confidence = Confidence(v)
		case "status":
			f.Status = Status(v)
		case "supersedes":
			f.Supersedes = v
		}
	}
	if err := f.Validate(); err != nil {
		return Fact{}, err
	}
	return f, nil
}

// splitFrontmatter splits "---\nkey: value...\n---\nbody" into a key/value
// map and the body. It is a deliberately small parser for our controlled
// format, not a general YAML implementation (zero-dependency rule).
func splitFrontmatter(s string) (map[string]string, string, error) {
	s = strings.TrimPrefix(s, "\uFEFF") // strip BOM
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, "", fmt.Errorf("ir: missing frontmatter open fence")
	}
	fm := map[string]string{}
	i := 1
	for ; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			return fm, strings.Join(lines[i+1:], "\n"), nil
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, v, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, "", fmt.Errorf("ir: bad frontmatter line %d: %q", i+1, line)
		}
		fm[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return nil, "", fmt.Errorf("ir: missing frontmatter close fence")
}

// SortFacts orders facts deterministically: by ID, then newest observation
// first. Used everywhere a fact list is rendered so output is reproducible.
func SortFacts(facts []Fact) {
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].ID != facts[j].ID {
			return facts[i].ID < facts[j].ID
		}
		return facts[i].ObservedAt.After(facts[j].ObservedAt)
	})
}
