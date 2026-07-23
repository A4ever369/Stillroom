// Package index builds a read-only, in-memory search index over the knowledge
// plane of many repos at once.
//
// Design rule (docs/design-v2.md §17): the server owns no source of truth.
// Every document here is derived from a `.team-context/` directory that lives
// in someone's git repo; the index is a cache that can be thrown away and
// rebuilt from git at any time. Nothing in this package writes to a repo, and
// nothing here ever touches the evidence plane — transcripts stay on the
// machine that produced them.
//
// Zero dependencies, like the rest of the project: the inverted index and the
// ranking below are deliberately small rather than a search engine.
package index

import (
	"sort"
	"strings"
	"time"

	"github.com/A4ever369/Stillroom/internal/ir"
	"github.com/A4ever369/Stillroom/internal/text"
)

// Kind distinguishes the two document types in the knowledge plane.
type Kind string

const (
	KindFact     Kind = "fact"
	KindPlaybook Kind = "playbook"
)

// Repo names one indexed knowledge base on disk.
type Repo struct {
	// Name is the display/URL name, e.g. "acme/management".
	Name string
	// Path is the repo root (the directory *containing* .team-context).
	Path string
}

// Doc is one indexed unit of knowledge — a fact or a playbook — flattened
// into the shape the search and web layers need.
type Doc struct {
	Kind  Kind
	Repo  string
	ID    string
	Title string // fact: its ID; playbook: its title
	Body  string
	Scope string

	Confidence ir.Confidence
	Status     ir.Status
	Source     string
	// At is observed_at for facts, updated_at for playbooks — the single
	// recency signal used for staleness and tie-breaking.
	At time.Time
}

// Stale reports whether the document has not been re-observed within d.
// Staleness is advisory: an old fact is not wrong, it is unverified.
func (d Doc) Stale(now time.Time, d0 time.Duration) bool { return now.Sub(d.At) > d0 }

// Index is an immutable snapshot. Rebuild to refresh; never mutate in place,
// so readers serving HTTP requests never see a half-built index.
type Index struct {
	docs     []Doc
	postings map[string][]int
	repos    []RepoStat
	BuiltAt  time.Time
	// Bad collects per-file parse failures keyed by "repo:filename".
	// Malformed files are reported, never fatal (tolerant-parsing rule).
	Bad map[string]error
}

// RepoStat summarizes one indexed repo for the sidebar and the health view.
type RepoStat struct {
	Name       string
	Facts      int
	Playbooks  int
	Newest     time.Time
	ParseFails int
}

// Build reads every repo's knowledge plane and returns a fresh snapshot.
// A repo that cannot be read at all is recorded in Bad and skipped — one
// broken checkout must not take down the whole index.
func Build(repos []Repo, now time.Time) *Index {
	ix := &Index{
		postings: map[string][]int{},
		BuiltAt:  now,
		Bad:      map[string]error{},
	}
	for _, r := range repos {
		st := ir.Store{Root: r.Path}
		stat := RepoStat{Name: r.Name}

		facts, badFacts, err := st.LoadFacts()
		if err != nil {
			ix.Bad[r.Name+":facts"] = err
		}
		for name, e := range badFacts {
			ix.Bad[r.Name+":facts/"+name] = e
			stat.ParseFails++
		}
		for _, f := range facts {
			ix.add(Doc{
				Kind: KindFact, Repo: r.Name, ID: f.ID, Title: f.ID,
				Body: f.Body, Scope: f.Scope, Confidence: f.Confidence,
				Status: f.Status, Source: f.Source, At: f.ObservedAt,
			})
			stat.Facts++
			if f.ObservedAt.After(stat.Newest) {
				stat.Newest = f.ObservedAt
			}
		}

		pbs, badPBs, err := st.LoadPlaybooks()
		if err != nil {
			ix.Bad[r.Name+":playbooks"] = err
		}
		for name, e := range badPBs {
			ix.Bad[r.Name+":playbooks/"+name] = e
			stat.ParseFails++
		}
		for _, p := range pbs {
			ix.add(Doc{
				Kind: KindPlaybook, Repo: r.Name, ID: p.ID, Title: p.Title,
				Body: p.Body, Status: ir.StatusActive, At: p.UpdatedAt,
				Source: strings.Join(p.Sources, ", "),
			})
			stat.Playbooks++
			if p.UpdatedAt.After(stat.Newest) {
				stat.Newest = p.UpdatedAt
			}
		}
		ix.repos = append(ix.repos, stat)
	}
	sort.Slice(ix.repos, func(i, j int) bool { return ix.repos[i].Name < ix.repos[j].Name })
	if len(ix.Bad) == 0 {
		ix.Bad = nil
	}
	return ix
}

func (ix *Index) add(d Doc) {
	n := len(ix.docs)
	ix.docs = append(ix.docs, d)
	seen := map[string]bool{}
	for _, tok := range text.Tokens(d.ID + " " + d.Title + " " + d.Body + " " + d.Scope) {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		ix.postings[tok] = append(ix.postings[tok], n)
	}
}

// Docs returns every indexed document, ordered deterministically.
func (ix *Index) Docs() []Doc { return ix.docs }

// Repos returns per-repo statistics, sorted by name.
func (ix *Index) Repos() []RepoStat { return ix.repos }

// Len is the total document count.
func (ix *Index) Len() int { return len(ix.docs) }

// Lookup finds one document by its coordinates.
func (ix *Index) Lookup(repo string, kind Kind, id string) (Doc, bool) {
	for _, d := range ix.docs {
		if d.Repo == repo && d.Kind == kind && d.ID == id {
			return d, true
		}
	}
	return Doc{}, false
}

// Filter narrows a search. Zero values mean "no constraint".
type Filter struct {
	Repo   string
	Kind   Kind
	Status ir.Status
	// StaleAfter, if non-zero, keeps only documents not re-observed within it.
	StaleAfter time.Duration
	Now        time.Time
	// Exclude drops one document by its coordinates. It must be applied as a
	// filter rather than after ranking: "related to X" searches X's own
	// vocabulary, so X is usually the only full-term match, and removing it
	// afterwards would leave an empty list instead of falling back to the
	// partial matches that are the actual answer.
	Exclude struct {
		Repo string
		Kind Kind
		ID   string
	}
}

func (f Filter) keep(d Doc) bool {
	if e := f.Exclude; e.ID != "" && d.ID == e.ID && d.Repo == e.Repo && d.Kind == e.Kind {
		return false
	}
	if f.Repo != "" && d.Repo != f.Repo {
		return false
	}
	if f.Kind != "" && d.Kind != f.Kind {
		return false
	}
	if f.Status != "" && d.Status != f.Status {
		return false
	}
	if f.StaleAfter > 0 && !d.Stale(f.Now, f.StaleAfter) {
		return false
	}
	return true
}

// Hit is a scored search result.
type Hit struct {
	Doc     Doc
	Score   int
	Snippet string
}

// Search ranks documents against a free-text query. An empty query lists
// everything matching the filter, newest first — the browse case.
//
// Ranking is intentionally simple and explainable: an ID match beats a title
// match beats a body match, and recency breaks ties. Documents matching every
// query token win outright; if nothing matches all of them, partial matches
// are returned rather than an empty page.
func (ix *Index) Search(q string, f Filter) []Hit {
	if f.Now.IsZero() {
		f.Now = ix.BuiltAt
	}
	terms := text.Tokens(q)
	if len(terms) == 0 {
		var hits []Hit
		for _, d := range ix.docs {
			if f.keep(d) {
				hits = append(hits, Hit{Doc: d, Snippet: snippet(d.Body, nil)})
			}
		}
		sortHits(hits)
		return hits
	}

	// Candidate set: any document containing at least one term.
	matched := map[int]int{} // doc -> how many distinct terms it contains
	for _, t := range dedupe(terms) {
		for _, id := range ix.postings[t] {
			matched[id]++
		}
	}

	full, partial := []Hit{}, []Hit{}
	want := len(dedupe(terms))
	for id, n := range matched {
		d := ix.docs[id]
		if !f.keep(d) {
			continue
		}
		h := Hit{Doc: d, Score: score(d, terms), Snippet: snippet(d.Body, terms)}
		if n == want {
			full = append(full, h)
		} else {
			h.Score = h.Score * n / want
			partial = append(partial, h)
		}
	}
	if len(full) > 0 {
		sortHits(full)
		return full
	}
	sortHits(partial)
	return partial
}

func score(d Doc, terms []string) int {
	idTok := set(text.Tokens(d.ID))
	titleTok := set(text.Tokens(d.Title))
	scopeTok := set(text.Tokens(d.Scope))
	body := strings.ToLower(d.Body)

	s := 0
	for _, t := range terms {
		switch {
		case idTok[t]:
			s += 8
		case titleTok[t]:
			s += 4
		case scopeTok[t]:
			s += 2
		}
		if n := strings.Count(body, t); n > 0 {
			s += min(n, 3)
		}
	}
	// High-confidence, active knowledge is what a teammate should see first.
	if d.Confidence == ir.ConfidenceHigh {
		s += 2
	}
	if d.Status != ir.StatusActive && d.Status != "" {
		s -= 4
	}
	return s
}

// sortHits orders by score, then recency, then a stable key — so the same
// index and query always produce byte-identical output (determinism rule).
func sortHits(hits []Hit) {
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if !a.Doc.At.Equal(b.Doc.At) {
			return a.Doc.At.After(b.Doc.At)
		}
		if a.Doc.Repo != b.Doc.Repo {
			return a.Doc.Repo < b.Doc.Repo
		}
		return a.Doc.ID < b.Doc.ID
	})
}

const snippetLen = 220

// snippet returns a short excerpt, centered on the first query term when one
// occurs in the body. Cuts land on rune boundaries so CJK never breaks.
func snippet(body string, terms []string) string {
	flat := strings.Join(strings.Fields(body), " ")
	if len(flat) <= snippetLen {
		return flat
	}
	at := 0
	low := strings.ToLower(flat)
	for _, t := range terms {
		if i := strings.Index(low, t); i >= 0 {
			at = i
			break
		}
	}
	start := at - snippetLen/3
	if start < 0 {
		start = 0
	}
	start = runeStart(flat, start)
	end := runeStart(flat, min(start+snippetLen, len(flat)))
	out := flat[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(flat) {
		out += "…"
	}
	return out
}

func runeStart(s string, i int) int {
	for i > 0 && i < len(s) && s[i]&0xC0 == 0x80 {
		i--
	}
	return i
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func set(in []string) map[string]bool {
	m := make(map[string]bool, len(in))
	for _, s := range in {
		m[s] = true
	}
	return m
}
