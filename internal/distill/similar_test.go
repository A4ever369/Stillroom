package distill

import (
	"testing"
	"time"

	"github.com/A4ever369/Stillroom/internal/ir"
)

func mkFact(id, body string) ir.Fact {
	return ir.Fact{
		ID: id, ObservedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Confidence: ir.ConfidenceHigh, Status: ir.StatusActive, Body: body,
	}
}

func TestSimilarExistingFlagsNearDuplicateChinese(t *testing.T) {
	existing := []ir.Fact{
		mkFact("deploy.acme.db-endpoint", "Acme 生产库入口是 pgbouncer,端口 6432,直连 5432 会被安全组拦。"),
		mkFact("ci.postgres.image", "CI 必须用 pgvector/pgvector:pg17 镜像。"),
	}
	dup := mkFact("deploy.acme.database-entry", "Acme 生产库的入口是 pgbouncer(端口 6432),直连 5432 被安全组拦截。")
	hits := SimilarExisting(dup, existing)
	if len(hits) != 1 || hits[0] != "deploy.acme.db-endpoint" {
		t.Fatalf("hits = %v, want the db-endpoint fact", hits)
	}
}

func TestSimilarExistingIgnoresSameIDAndUnrelated(t *testing.T) {
	existing := []ir.Fact{
		mkFact("deploy.acme.db-endpoint", "Acme 生产库入口是 pgbouncer,端口 6432。"),
	}
	// Same id → supersession, not duplication.
	same := mkFact("deploy.acme.db-endpoint", "Acme 生产库入口是 pgbouncer,端口 6432,新增说明。")
	if hits := SimilarExisting(same, existing); hits != nil {
		t.Fatalf("same-id should be skipped, got %v", hits)
	}
	// Unrelated body → no hit.
	other := mkFact("build.web.pnpm", "web 目录用 pnpm workspace,node 版本锁 22。")
	if hits := SimilarExisting(other, existing); hits != nil {
		t.Fatalf("unrelated should not hit, got %v", hits)
	}
}

// The cases below are modelled on duplicates found in real distillation output
// (21 sessions across four repos, 2026-07-23). The wording is invented — the
// *shapes* are what was observed, and each one defeated the previous
// rune-bigram signal.

// The model re-derives an id per session, so the same knowledge lands under
// the same words in a different order. Zero-cost to catch, and it fires even
// when the two bodies share almost no vocabulary.
func TestSimilarExistingCatchesIDWordOrderVariants(t *testing.T) {
	existing := []ir.Fact{
		mkFact("discovery.delete-environment.cascade",
			"Deleting an environment removes its scans and findings through the FK cascade."),
		mkFact("build.web.node-version", "The web workspace pins Node 22."),
	}
	// Same id tokens, different order, deliberately different wording.
	dup := mkFact("discovery.environment-delete.cascade",
		"Removing an env row also drops every dependent record; the constraint is ON DELETE CASCADE.")

	hits := SimilarExisting(dup, existing)
	if len(hits) != 1 || hits[0] != "discovery.delete-environment.cascade" {
		t.Fatalf("hits = %v, want the word-order variant", hits)
	}
}

// A real duplicate the old rune-bigram threshold (0.55) scored at 0.53 and
// therefore missed: same failure, different id vocabulary.
func TestSimilarExistingCatchesDifferentlyNamedDuplicate(t *testing.T) {
	existing := []ir.Fact{
		mkFact("build.next-font.network-required",
			"next build fails hard when offline because next/font fetches Google Fonts at build time; "+
				"there is no cached fallback, so an air-gapped build cannot complete."),
	}
	dup := mkFact("build.next.google-fonts-network",
		"Building with Turbopack requires network access: next/font downloads Google Fonts during the "+
			"build and fails the whole build when the fetch cannot complete offline.")

	if hits := SimilarExisting(dup, existing); len(hits) != 1 {
		t.Fatalf("hits = %v, want the differently-named duplicate flagged", hits)
	}
}

// The regression that matters most: unrelated facts from the same project
// share enormous character-level material, which is why the rune-bigram signal
// put them at 0.44-0.53 — inside its own duplicate band. None may be flagged.
func TestSimilarExistingDoesNotFlagUnrelatedSameDomainFacts(t *testing.T) {
	existing := []ir.Fact{
		mkFact("build.pnpm.deps-status-check",
			"Running pnpm scripts in this repo triggers a dependency status check that fails "+
				"non-interactively unless the build scripts are approved first."),
		mkFact("i18n.locale-key-parity",
			"Every locale file must carry the same key set; a missing key in one locale makes the "+
				"type check fail rather than falling back at runtime."),
		mkFact("db.migrations.scoping-convention",
			"Migration files are named with the feature prefix so that unrelated features can land "+
				"migrations in the same release without renumbering each other."),
		mkFact("repo.commit-identity",
			"Commits from this machine must use the project identity; the global git identity is a "+
				"different account and will be rejected by the push hook."),
	}
	probe := mkFact("tests.known-failing.baseline",
		"Three suite files fail on a clean checkout for reasons unrelated to any change; treat that "+
			"count as the baseline rather than a regression introduced by your work.")

	if hits := SimilarExisting(probe, existing); hits != nil {
		t.Fatalf("unrelated same-domain facts must not be flagged, got %v", hits)
	}
}

// Output order must not depend on map iteration.
func TestSimilarExistingIsDeterministic(t *testing.T) {
	body := "The production database is reached through pgbouncer on port 6432; direct connections " +
		"to 5432 are blocked by the security group."
	existing := []ir.Fact{
		mkFact("z.deploy.db-entry", body),
		mkFact("a.deploy.db-entry", body),
		mkFact("m.deploy.db-entry", body),
	}
	probe := mkFact("deploy.acme.db-endpoint", body)

	first := SimilarExisting(probe, existing)
	if len(first) != 3 {
		t.Fatalf("want all three flagged, got %v", first)
	}
	for i := 0; i < 20; i++ {
		got := SimilarExisting(probe, existing)
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d differs: %v vs %v", i, first, got)
			}
		}
	}
	if first[0] != "a.deploy.db-entry" {
		t.Errorf("results should be sorted, got %v", first)
	}
}

// An empty or whitespace body cannot be compared; it must not panic and must
// not match everything.
func TestSimilarExistingHandlesEmptyBody(t *testing.T) {
	existing := []ir.Fact{mkFact("some.fact", "A real body with real words in it.")}
	if hits := SimilarExisting(mkFact("other.fact", "   "), existing); hits != nil {
		t.Fatalf("empty body should match nothing, got %v", hits)
	}
}
