package ir

// Fuzzing the knowledge-file parsers (docs/testing.md L2, "tolerant parsing").
//
// These files are edited by hand, merged by git, and occasionally left with
// conflict markers in them — the parser sees far more than what Encode writes.
// The contract is: never panic, and never accept something you would then
// re-encode differently.

import (
	"strings"
	"testing"
	"time"
)

func FuzzParseFact(f *testing.F) {
	valid := Fact{
		ID: "deploy.acme.db-endpoint", Scope: "global", Source: "claude-code://s",
		ObservedAt: time.Unix(1700000000, 0).UTC(),
		Confidence: ConfidenceHigh, Status: StatusActive,
		Body: "Prod DB entry is pgbouncer on 6432.",
	}
	f.Add(string(valid.Encode()))
	f.Add("")
	f.Add("---\n---\n")
	f.Add("no frontmatter at all, just prose")
	f.Add("---\nid: a.b\n---\nbody")
	f.Add("---\nid: a.b\nobserved_at: not-a-date\n---\nbody")
	// What an unresolved git merge leaves behind.
	f.Add("---\nid: a.b\n<<<<<<< HEAD\nbody: mine\n=======\nbody: theirs\n>>>>>>> other\n---\n")
	f.Add("---\nid: 上手.中文\nstatus: active\n---\n中文正文")
	f.Add(strings.Repeat("---\n", 500))

	f.Fuzz(func(t *testing.T, data string) {
		fact, err := ParseFact([]byte(data))
		if err != nil {
			return // rejecting input is always allowed
		}
		// Anything accepted must be re-encodable to something that parses
		// back identically — otherwise a hand-edited file silently mutates
		// the first time the tool rewrites it.
		encoded := fact.Encode()
		again, err := ParseFact(encoded)
		if err != nil {
			t.Fatalf("re-parsing our own encoding failed\ninput: %q\nencoded: %q\nerr: %v", data, encoded, err)
		}
		if got := again.Encode(); string(got) != string(encoded) {
			t.Fatalf("encode is not a fixed point\ninput: %q\npass 1: %q\npass 2: %q", data, encoded, got)
		}
		// An accepted fact must also be a writable fact: the loader and the
		// writer must agree on what is valid, or LoadFacts can produce facts
		// that WriteFact then refuses.
		if err := fact.Validate(); err != nil {
			t.Fatalf("ParseFact accepted a fact that Validate rejects: %v\ninput: %q", err, data)
		}
	})
}

func FuzzParsePlaybook(f *testing.F) {
	valid := Playbook{
		ID: "acme-deploy", Title: "Acme deploy",
		UpdatedAt: time.Unix(1700000000, 0).UTC(),
		Body:      "## Steps\n1. make deploy-prod",
	}
	f.Add(string(valid.Encode()))
	f.Add("")
	f.Add("---\nid: acme-deploy\n---\n")
	f.Add("---\nid: acme-deploy\ntitle: T\nupdated_at: yesterday\n---\nbody")
	f.Add("---\nid: a\n<<<<<<< HEAD\ntitle: mine\n=======\ntitle: theirs\n>>>>>>> b\n---\n")

	f.Fuzz(func(t *testing.T, data string) {
		pb, err := ParsePlaybook([]byte(data))
		if err != nil {
			return
		}
		encoded := pb.Encode()
		again, err := ParsePlaybook(encoded)
		if err != nil {
			t.Fatalf("re-parsing our own encoding failed\ninput: %q\nencoded: %q\nerr: %v", data, encoded, err)
		}
		if got := again.Encode(); string(got) != string(encoded) {
			t.Fatalf("encode is not a fixed point\ninput: %q\npass 1: %q\npass 2: %q", data, encoded, got)
		}
		if err := pb.Validate(); err != nil {
			t.Fatalf("ParsePlaybook accepted a playbook that Validate rejects: %v\ninput: %q", err, data)
		}
	})
}
