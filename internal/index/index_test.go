package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/A4ever369/Stillroom/internal/ir"
	"github.com/A4ever369/Stillroom/internal/text"
)

var now = time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

// repo writes a knowledge base under a temp dir and returns the Repo entry.
func repo(t *testing.T, name string, facts []ir.Fact, pbs []ir.Playbook) Repo {
	t.Helper()
	root := t.TempDir()
	st := ir.Store{Root: root}
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	for _, f := range facts {
		if err := st.WriteFact(f); err != nil {
			t.Fatalf("write fact %s: %v", f.ID, err)
		}
	}
	for _, p := range pbs {
		if err := st.WritePlaybook(p); err != nil {
			t.Fatalf("write playbook %s: %v", p.ID, err)
		}
	}
	return Repo{Name: name, Path: root}
}

func fact(id, body string, age time.Duration) ir.Fact {
	return ir.Fact{
		ID: id, Scope: "repo:demo", ObservedAt: now.Add(-age),
		Source: "claude-code://abc", Confidence: ir.ConfidenceHigh,
		Status: ir.StatusActive, Body: body,
	}
}

func ids(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Doc.ID
	}
	return out
}

// The index is a pure projection of what is on disk: every fact and playbook
// in every repo shows up exactly once, attributed to its repo.
func TestBuildIndexesEveryRepo(t *testing.T) {
	a := repo(t, "acme/infra", []ir.Fact{
		fact("deploy.db.endpoint", "Production Postgres is reached via pgbouncer on 6432.", 0),
	}, nil)
	b := repo(t, "acme/web", []ir.Fact{
		fact("ci.node.version", "CI pins Node 22; 20 fails the build.", 0),
	}, []ir.Playbook{{
		ID: "ship-a-release", Title: "Ship a release", UpdatedAt: now,
		Body: "Tag, wait for CI, promote.",
	}})

	ix := Build([]Repo{a, b}, now)
	if ix.Len() != 3 {
		t.Fatalf("want 3 documents, got %d", ix.Len())
	}
	if len(ix.Repos()) != 2 {
		t.Fatalf("want 2 repos, got %d", len(ix.Repos()))
	}
	if s := ix.Repos()[1]; s.Name != "acme/web" || s.Facts != 1 || s.Playbooks != 1 {
		t.Errorf("bad repo stat: %+v", s)
	}
	if _, ok := ix.Lookup("acme/infra", KindFact, "deploy.db.endpoint"); !ok {
		t.Error("fact not findable by coordinates")
	}
}

// A malformed file is reported, never fatal: one bad merge in one repo must
// not take the whole org's index down (tolerant-parsing rule).
func TestBuildSurvivesMalformedFiles(t *testing.T) {
	r := repo(t, "acme/infra", []ir.Fact{fact("good.one", "Still readable.", 0)}, nil)
	bad := filepath.Join(r.Path, ir.DirName, "facts", "broken.md")
	if err := os.WriteFile(bad, []byte("not frontmatter at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	ix := Build([]Repo{r}, now)
	if ix.Len() != 1 {
		t.Fatalf("good fact should still be indexed, got %d docs", ix.Len())
	}
	if len(ix.Bad) != 1 {
		t.Errorf("the malformed file should be reported, got %v", ix.Bad)
	}
	if ix.Repos()[0].ParseFails != 1 {
		t.Errorf("parse failure should surface in the repo stat: %+v", ix.Repos()[0])
	}
}

// A repo that does not exist at all is simply empty — the server keeps serving.
func TestBuildToleratesMissingRepo(t *testing.T) {
	ix := Build([]Repo{{Name: "gone", Path: filepath.Join(t.TempDir(), "nope")}}, now)
	if ix.Len() != 0 {
		t.Fatalf("want 0 documents, got %d", ix.Len())
	}
	if len(ix.Repos()) != 1 {
		t.Error("the repo should still be listed, with zero counts")
	}
}

// Ranking is explainable: a match in the fact ID outranks a match that only
// appears in some other fact's body.
func TestSearchRanksIDMatchesFirst(t *testing.T) {
	r := repo(t, "acme/infra", []ir.Fact{
		fact("ci.postgres.image", "Use the pgvector image.", 0),
		fact("unrelated.note", "Someone mentioned postgres once in passing.", 0),
	}, nil)

	got := ids(Build([]Repo{r}, now).Search("postgres", Filter{Now: now}))
	if len(got) != 2 || got[0] != "ci.postgres.image" {
		t.Errorf("want the ID match first, got %v", got)
	}
}

// Multi-term queries prefer documents containing every term, but never return
// an empty page when only partial matches exist.
func TestSearchPrefersFullMatchesButFallsBack(t *testing.T) {
	r := repo(t, "acme/infra", []ir.Fact{
		fact("a.both", "The deploy key rotated last week.", 0),
		fact("b.one", "The deploy runs on push to main.", 0),
	}, nil)
	ix := Build([]Repo{r}, now)

	if got := ids(ix.Search("deploy key", Filter{Now: now})); len(got) != 1 || got[0] != "a.both" {
		t.Errorf("full match should win outright, got %v", got)
	}
	if got := ids(ix.Search("deploy nonexistentword", Filter{Now: now})); len(got) != 2 {
		t.Errorf("partial matches should still be returned, got %v", got)
	}
}

// Chinese has no spaces. Character bigrams make CJK knowledge searchable
// without a segmenter — a first-class case, not an edge case.
func TestSearchFindsCJK(t *testing.T) {
	r := repo(t, "acme/infra", []ir.Fact{
		fact("db.backup", "重启前必须备份数据库，否则迁移不可逆。", 0),
		fact("ci.cache", "构建缓存放在 /var/cache 下。", 0),
	}, nil)

	got := ids(Build([]Repo{r}, now).Search("数据库", Filter{Now: now}))
	if len(got) != 1 || got[0] != "db.backup" {
		t.Errorf("CJK query should match the CJK fact, got %v", got)
	}
}

// An empty query is the browse case: everything, newest first.
func TestEmptyQueryBrowsesNewestFirst(t *testing.T) {
	r := repo(t, "acme/infra", []ir.Fact{
		fact("old.one", "Learned a while ago.", 200*24*time.Hour),
		fact("new.one", "Learned today.", 0),
	}, nil)

	got := ids(Build([]Repo{r}, now).Search("", Filter{Now: now}))
	if len(got) != 2 || got[0] != "new.one" {
		t.Errorf("want newest first, got %v", got)
	}
}

func TestFilters(t *testing.T) {
	a := repo(t, "acme/infra", []ir.Fact{
		fact("fresh.fact", "Recently confirmed.", 0),
		fact("stale.fact", "Nobody has re-observed this.", 300*24*time.Hour),
	}, []ir.Playbook{{ID: "a-book", Title: "A book", UpdatedAt: now, Body: "Steps."}})
	b := repo(t, "acme/web", []ir.Fact{fact("other.repo.fact", "Elsewhere.", 0)}, nil)
	ix := Build([]Repo{a, b}, now)

	t.Run("repo", func(t *testing.T) {
		if got := ix.Search("", Filter{Repo: "acme/web", Now: now}); len(got) != 1 {
			t.Errorf("want 1 doc from acme/web, got %v", ids(got))
		}
	})
	t.Run("kind", func(t *testing.T) {
		if got := ix.Search("", Filter{Kind: KindPlaybook, Now: now}); len(got) != 1 {
			t.Errorf("want 1 playbook, got %v", ids(got))
		}
	})
	t.Run("staleness", func(t *testing.T) {
		got := ix.Search("", Filter{StaleAfter: 180 * 24 * time.Hour, Now: now})
		if len(got) != 1 || got[0].Doc.ID != "stale.fact" {
			t.Errorf("want only the unverified fact, got %v", ids(got))
		}
	})
}

// Same index + same query must produce the same order every time — the
// determinism rule extends to what the server renders.
func TestSearchIsDeterministic(t *testing.T) {
	var facts []ir.Fact
	for _, id := range []string{"a.one", "b.two", "c.three", "d.four"} {
		facts = append(facts, fact(id, "identical body text for tie-breaking", 0))
	}
	ix := Build([]Repo{repo(t, "acme/infra", facts, nil)}, now)

	first := ids(ix.Search("identical", Filter{Now: now}))
	for i := 0; i < 20; i++ {
		if got := ids(ix.Search("identical", Filter{Now: now})); strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("run %d differs:\n%v\n%v", i, first, got)
		}
	}
}

// Snippets must never cut a multibyte rune in half.
func TestSnippetKeepsRuneBoundaries(t *testing.T) {
	body := strings.Repeat("重启前必须备份数据库，否则迁移不可逆。", 20)
	for _, term := range []string{"", "数据库"} {
		var terms []string
		if term != "" {
			terms = text.Tokens(term)
		}
		s := snippet(body, terms)
		if !isValidUTF8(s) {
			t.Errorf("snippet for %q broke a rune: %q", term, s)
		}
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

// Superseded knowledge still exists for lineage, but must not outrank the
// active fact that replaced it.
func TestSupersededRanksBelowActive(t *testing.T) {
	active := fact("host.address", "The box is at 10.0.0.9.", 0)
	old := fact("old.host.address", "The box is at 10.0.0.1.", 0)
	old.Status = ir.StatusSuperseded
	ix := Build([]Repo{repo(t, "acme/infra", []ir.Fact{active, old}, nil)}, now)

	got := ids(ix.Search("box address", Filter{Now: now}))
	if len(got) == 0 || got[0] != "host.address" {
		t.Errorf("active fact should rank first, got %v", got)
	}
}

// "Related to X" searches X's own vocabulary, so X is normally the only
// full-term match. Excluding it must happen during filtering, not after
// ranking — otherwise the panel is always empty (regression guard).
func TestExcludeFallsBackToPartialMatches(t *testing.T) {
	r := repo(t, "acme/infra", []ir.Fact{
		fact("deploy.ci.secrets", "The deploy key went stale after the box migrated.", 0),
		fact("deploy.box.host", "The box moved to a new address.", 0),
	}, nil)
	ix := Build([]Repo{r}, now)

	f := Filter{Now: now}
	f.Exclude.Repo, f.Exclude.Kind, f.Exclude.ID = "acme/infra", KindFact, "deploy.ci.secrets"

	got := ids(ix.Search("deploy.ci.secrets", f))
	if len(got) != 1 || got[0] != "deploy.box.host" {
		t.Errorf("want the neighbouring fact, got %v", got)
	}
}
