package distill

// Fuzzing the proposal parser (docs/testing.md L2).
//
// This is the least controllable input in the system: a language model's free
// text, on its way to becoming files in a shared git repo. The contract is
// that anything surviving the parse is already valid AND already redacted —
// callers write straight to disk without re-checking.

import (
	"strings"
	"testing"
	"time"

	"github.com/A4ever369/Stillroom/internal/ir"
	"github.com/A4ever369/Stillroom/internal/redact"
)

func FuzzParseProposal(f *testing.F) {
	f.Add(`{"facts":[{"id":"a.b","confidence":"high","body":"something durable"}]}`)
	f.Add("")
	f.Add("Sure! Here is what I found:")
	f.Add("```json\n{\"facts\":[]}\n```")
	f.Add(`{"facts":[{"id":"../../etc/passwd","confidence":"high","body":"x"}]}`)
	f.Add(`{"facts":[{"id":"a.b","confidence":"WILD","body":"x"}]}`)
	f.Add(`{"facts":[{"id":"a.b","confidence":"high","body":"key is AKIAIOSFODNN7EXAMPLE"}]}`)
	f.Add(`{"facts":null,"playbook":null}`)
	f.Add(`{"facts":[{"id":"a.b","body":"` + strings.Repeat("x", 10000) + `"}]}`)
	f.Add(`{"playbook":{"id":"p","title":"T","body":"## Steps"}}`)
	f.Add("{" + strings.Repeat(`"a":{`, 100) + strings.Repeat("}", 101))

	opts := Options{
		Scope:     "repo:test",
		SourceRef: "claude-code://fuzz",
		Now:       time.Unix(1700000000, 0).UTC(),
	}

	f.Fuzz(func(t *testing.T, raw string) {
		prop, err := parseProposal(raw, opts)
		if err != nil {
			return // rejecting model output is always allowed
		}

		// Everything that survives must be writable as-is: callers hand these
		// straight to Store.WriteFact without re-validating.
		for _, fact := range prop.Facts {
			if err := fact.Validate(); err != nil {
				t.Fatalf("parseProposal returned an unwritable fact: %v\nid: %q\ninput: %q", err, fact.ID, raw)
			}
			if fact.Filename() != fact.ID+".md" || strings.ContainsAny(fact.ID, `/\`) {
				t.Fatalf("fact id would escape the knowledge directory: %q\ninput: %q", fact.ID, raw)
			}
			if fact.Status != ir.StatusActive {
				t.Fatalf("distilled fact should start active, got %q", fact.Status)
			}
			// Redaction runs on distiller output, not just on prompts: this
			// is where credential-shaped strings gather.
			if scrubbed, n := redact.Text(fact.Body); n > 0 {
				t.Fatalf("unredacted secret survived into a fact body\nbody: %q\nwould become: %q\ninput: %q",
					fact.Body, scrubbed, raw)
			}
		}

		if prop.Playbook != nil {
			if err := prop.Playbook.Validate(); err != nil {
				t.Fatalf("parseProposal returned an unwritable playbook: %v\ninput: %q", err, raw)
			}
			if scrubbed, n := redact.Text(prop.Playbook.Body); n > 0 {
				t.Fatalf("unredacted secret survived into a playbook body\nbody: %q\nwould become: %q\ninput: %q",
					prop.Playbook.Body, scrubbed, raw)
			}
		}
	})
}
