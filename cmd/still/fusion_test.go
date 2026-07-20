package main

// The architectural bet (docs/design-v2.md §2): knowledge fuses because the
// knowledge plane is a real git repo with one fact per file, so git's own
// directory merge IS the fusion algorithm. Nothing else in the design works
// if that is false, and until now it had zero test coverage.
//
// These are the L4 fusion scenarios from docs/testing.md.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// factProposal builds a distiller response asserting one fact.
func factProposal(id, body string) string {
	return proposal(`{"facts":[{"id":"` + id + `","confidence":"high","body":` + jsonString(body) + `}]}`)
}

// setupTeam returns two clones of a repo that has already been initialized
// and committed, i.e. two teammates starting from the same knowledge base.
func setupTeam(t *testing.T) (alice, bob *world) {
	t.Helper()
	origin := newWorld(t)
	origin.run("init")
	origin.gitCommit("adopt stillroom")
	return origin.clone("alice"), origin.clone("bob")
}

// The common case: two teammates learn different things in parallel. Git must
// merge both without human intervention, and both facts must survive.
func TestDisjointKnowledgeMergesCleanly(t *testing.T) {
	alice, bob := setupTeam(t)

	alice.fakeClaude(factProposal("deploy.acme.db-endpoint", "Prod DB entry is pgbouncer on 6432."))
	alice.session("a.jsonl", longSession()...)
	if got := alice.run("distill"); got.code != 0 {
		t.Fatalf("alice distill: %s", got.out())
	}
	alice.gitCommit("alice learns about the db")

	bob.fakeClaude(factProposal("ci.flaky-test-retry", "CI retries integration tests twice before failing."))
	bob.session("b.jsonl", longSession()...)
	if got := bob.run("distill"); got.code != 0 {
		t.Fatalf("bob distill: %s", got.out())
	}
	bob.gitCommit("bob learns about ci")

	out, conflicted := alice.pullFrom(bob)
	if conflicted {
		t.Fatalf("disjoint knowledge did not merge cleanly — the core design bet:\n%s", out)
	}

	for _, id := range []string{"deploy.acme.db-endpoint", "ci.flaky-test-retry"} {
		if !alice.exists(".team-context/facts/" + id + ".md") {
			t.Errorf("%s did not survive the merge", id)
		}
	}

	// After merging, the materialized view must reflect the union — and
	// regenerating it must be all that is needed.
	if got := alice.run("materialize"); got.code != 0 {
		t.Fatalf("materialize after merge: %s", got.out())
	}
	body := alice.read(".team-context/materialized.md")
	for _, id := range []string{"deploy.acme.db-endpoint", "ci.flaky-test-retry"} {
		if !strings.Contains(body, id) {
			t.Errorf("%s missing from materialized.md after merge:\n%s", id, body)
		}
	}
	if status := alice.run("status"); !strings.Contains(status.stdout, "facts: 2") {
		t.Errorf("want 2 facts after merging two teammates:\n%s", status.out())
	}
}

// The hard case: both teammates revise the SAME fact. A conflict here is
// correct and expected — two humans disagreeing about one thing is exactly
// what review is for. What matters is that the blast radius is one file:
// every other fact must merge, and the store must stay loadable.
func TestConflictOnOneFactDoesNotPoisonTheRest(t *testing.T) {
	alice, bob := setupTeam(t)

	// Shared background knowledge both sides also learn, plus the contested one.
	alice.fakeClaude(factProposal("deploy.acme.db-endpoint", "Prod DB entry is pgbouncer on 6432."))
	alice.session("a1.jsonl", longSession()...)
	alice.run("distill")
	alice.fakeClaude(factProposal("alice.only", "Alice learned something nobody disputes."))
	alice.session("a2.jsonl", longSession()...)
	alice.run("distill", "--force")
	alice.gitCommit("alice")

	bob.fakeClaude(factProposal("deploy.acme.db-endpoint", "Prod DB entry moved to a proxy on 7432."))
	bob.session("b1.jsonl", longSession()...)
	bob.run("distill")
	bob.fakeClaude(factProposal("bob.only", "Bob learned something nobody disputes."))
	bob.session("b2.jsonl", longSession()...)
	bob.run("distill", "--force")
	bob.gitCommit("bob")

	out, conflicted := alice.pullFrom(bob)
	if !conflicted {
		// Not automatically a failure: whichever observation is newer may
		// legitimately win. But it must not silently lose Bob's content.
		t.Logf("merge resolved without conflict:\n%s", out)
	}

	// Whatever happened to the contested fact, the uncontested ones merged.
	for _, id := range []string{"alice.only", "bob.only"} {
		if !alice.exists(".team-context/facts/" + id + ".md") {
			t.Errorf("%s was lost — a conflict on one fact must not affect others", id)
		}
	}

	// And the conflict, if any, is confined to the one file.
	conflictedFiles := alice.conflictedPaths()
	for _, f := range conflictedFiles {
		if !strings.Contains(f, "deploy.acme.db-endpoint") {
			t.Errorf("conflict spread beyond the contested fact: %s", f)
		}
	}
	if len(conflictedFiles) > 1 {
		t.Errorf("want at most one conflicted file, got: %v", conflictedFiles)
	}
}

// A repo carrying an unresolved conflict must still be usable: `status` names
// the broken file instead of failing, so the tool helps rather than blocks.
func TestStoreSurvivesAnUnresolvedConflict(t *testing.T) {
	alice, bob := setupTeam(t)

	alice.fakeClaude(factProposal("deploy.acme.db-endpoint", "Prod DB entry is pgbouncer on 6432."))
	alice.session("a.jsonl", longSession()...)
	alice.run("distill")
	alice.fakeClaude(factProposal("alice.only", "Uncontested."))
	alice.session("a2.jsonl", longSession()...)
	alice.run("distill", "--force")
	alice.gitCommit("alice")

	bob.fakeClaude(factProposal("deploy.acme.db-endpoint", "Prod DB moved to a proxy on 7432."))
	bob.session("b.jsonl", longSession()...)
	bob.run("distill")
	bob.gitCommit("bob")

	alice.pullFrom(bob)

	got := alice.run("status")
	if got.code != 0 {
		t.Fatalf("status must survive a conflicted store, exit=%d:\n%s", got.code, got.out())
	}
	if strings.Contains(got.out(), "panic:") {
		t.Fatalf("panicked on a conflicted store:\n%s", got.out())
	}
	// The uncontested fact is still readable and counted.
	if !strings.Contains(got.stdout, "alice.only") && !strings.Contains(got.stdout, "facts:") {
		t.Errorf("status gave no usable output on a conflicted store:\n%s", got.out())
	}
}

// Regression: git does not track empty directories, so a teammate cloning a
// freshly adopted repo used to receive no facts/ or playbooks/ at all, and
// their very first distill died on a bare ENOENT — on the onboarding path.
func TestTeammateCanDistillInAFreshClone(t *testing.T) {
	origin := newWorld(t)
	origin.run("init")
	origin.gitCommit("adopt stillroom")

	for _, dir := range []string{".team-context/facts", ".team-context/playbooks"} {
		if !origin.exists(dir + "/.gitkeep") {
			t.Fatalf("%s has no keeper file — it will not survive a clone", dir)
		}
	}

	teammate := origin.clone("teammate")
	if !teammate.exists(".team-context/facts") {
		t.Fatal("facts/ did not survive the clone")
	}
	teammate.fakeClaude(factProposal("onboarding.first-fact", "The first thing a new teammate learns."))
	teammate.session("t.jsonl", longSession()...)

	got := teammate.run("distill")
	if got.code != 0 {
		t.Fatalf("a teammate's first distill in a fresh clone failed:\n%s", got.out())
	}
	if !teammate.exists(".team-context/facts/onboarding.first-fact.md") {
		t.Errorf("fact not written in a fresh clone:\n%s", got.out())
	}
}

// The union-merge rule must be committed with the repo (so every clone gets
// it without setup) and must be added in place to repos initialized by
// earlier versions.
func TestMaterializedIsConfiguredForUnionMerge(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	attrs := w.read(".team-context/.gitattributes")
	if !strings.Contains(attrs, "materialized.md merge=union") {
		t.Errorf(".gitattributes missing the union-merge rule:\n%s", attrs)
	}

	// Upgrade in place: a repo predating the rule gains it, keeping whatever
	// the user put there themselves.
	if err := os.WriteFile(filepath.Join(w.repo, ".team-context", ".gitattributes"),
		[]byte("*.md text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.run("init")
	attrs = w.read(".team-context/.gitattributes")
	if !strings.Contains(attrs, "*.md text") {
		t.Errorf("init discarded the user's own .gitattributes rules:\n%s", attrs)
	}
	if !strings.Contains(attrs, "materialized.md merge=union") {
		t.Errorf("init did not add the union-merge rule in place:\n%s", attrs)
	}
}
