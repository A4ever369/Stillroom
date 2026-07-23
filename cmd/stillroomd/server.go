package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/A4ever369/Stillroom/internal/index"
	"github.com/A4ever369/Stillroom/internal/ir"
)

//go:embed web
var webFS embed.FS

// staticFS is the CSS/asset subtree. Everything the browser needs ships inside
// the binary, so the container has no asset volume and no CDN dependency.
var staticFS = func() fs.FS {
	sub, err := fs.Sub(webFS, "web/static")
	if err != nil {
		panic(err)
	}
	return sub
}()

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"ago":  humanAgo,
	"date": func(t time.Time) string { return t.Format("2006-01-02") },
	"add":  func(a, b int) int { return a + b },
	"stale": func(d index.Doc, days int) bool {
		return d.Stale(time.Now(), time.Duration(days)*24*time.Hour)
	},
	"qs": queryString,
}).ParseFS(webFS, "web/*.html"))

// queryString rebuilds the current search URL with one parameter replaced —
// so toggling a facet never silently drops the query or the other filters.
func queryString(q searchQuery, key, value string) template.URL {
	v := url.Values{}
	set := func(k, cur string) {
		if k == key {
			cur = value
		}
		if cur != "" {
			v.Set(k, cur)
		}
	}
	set("q", q.Q)
	set("repo", q.Repo)
	set("kind", string(q.Kind))
	set("status", string(q.Status))
	stale := ""
	if q.Stale > 0 {
		stale = strconv.Itoa(q.Stale)
	}
	set("stale", stale)
	if len(v) == 0 {
		return "/"
	}
	return template.URL("/?" + v.Encode())
}

const maxResults = 100

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleSearch)
	mux.HandleFunc("GET /d", s.handleDoc)
	mux.HandleFunc("GET /api/search", s.handleAPISearch)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	return logging(mux)
}

// searchQuery is the parsed, validated form of the query string — one place
// that turns untrusted input into an index.Filter.
type searchQuery struct {
	Q      string
	Repo   string
	Kind   index.Kind
	Status ir.Status
	Stale  int // days; 0 = no staleness constraint
}

func parseQuery(r *http.Request) searchQuery {
	q := searchQuery{
		Q:    strings.TrimSpace(r.URL.Query().Get("q")),
		Repo: r.URL.Query().Get("repo"),
	}
	switch k := index.Kind(r.URL.Query().Get("kind")); k {
	case index.KindFact, index.KindPlaybook:
		q.Kind = k
	}
	switch st := ir.Status(r.URL.Query().Get("status")); st {
	case ir.StatusActive, ir.StatusSuperseded, ir.StatusDisputed:
		q.Status = st
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("stale")); err == nil && n > 0 {
		q.Stale = n
	}
	return q
}

func (q searchQuery) filter(now time.Time) index.Filter {
	return index.Filter{
		Repo:       q.Repo,
		Kind:       q.Kind,
		Status:     q.Status,
		StaleAfter: time.Duration(q.Stale) * 24 * time.Hour,
		Now:        now,
	}
}

type searchPage struct {
	Query     searchQuery
	Hits      []index.Hit
	Total     int
	Truncated bool
	Repos     []index.RepoStat
	Docs      int
	BuiltAt   time.Time
	Now       time.Time
	Version   string
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ix := s.index()
	now := time.Now()
	q := parseQuery(r)

	hits := ix.Search(q.Q, q.filter(now))
	page := searchPage{
		Query: q, Total: len(hits), Repos: ix.Repos(),
		Docs: ix.Len(), BuiltAt: ix.BuiltAt, Now: now, Version: version,
	}
	if len(hits) > maxResults {
		hits, page.Truncated = hits[:maxResults], true
	}
	page.Hits = hits
	render(w, "search.html", page)
}

type docPage struct {
	Doc     index.Doc
	Related []index.Hit
	Now     time.Time
	Version string
}

func (s *server) handleDoc(w http.ResponseWriter, r *http.Request) {
	ix := s.index()
	qs := r.URL.Query()
	kind := index.Kind(qs.Get("kind"))
	doc, ok := ix.Lookup(qs.Get("repo"), kind, qs.Get("id"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// "Related" is the same search the user would have run: other knowledge
	// that shares vocabulary with this one, across every repo. This is where
	// cross-repo value shows up — the fact your team never wrote.
	relatedFilter := index.Filter{Now: time.Now()}
	relatedFilter.Exclude.Repo = doc.Repo
	relatedFilter.Exclude.Kind = doc.Kind
	relatedFilter.Exclude.ID = doc.ID
	related := ix.Search(doc.Title+" "+doc.Scope, relatedFilter)
	if len(related) > 8 {
		related = related[:8]
	}
	render(w, "doc.html", docPage{Doc: doc, Related: related, Now: time.Now(), Version: version})
}

// handleAPISearch is the machine-readable surface — the same ranking the UI
// shows, so an agent or a CI job can query the org's knowledge directly.
func (s *server) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	ix := s.index()
	q := parseQuery(r)
	hits := ix.Search(q.Q, q.filter(time.Now()))
	if len(hits) > maxResults {
		hits = hits[:maxResults]
	}
	type item struct {
		Kind       string `json:"kind"`
		Repo       string `json:"repo"`
		ID         string `json:"id"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		Scope      string `json:"scope,omitempty"`
		Confidence string `json:"confidence,omitempty"`
		Status     string `json:"status,omitempty"`
		At         string `json:"at"`
		Score      int    `json:"score"`
	}
	out := struct {
		Query   string `json:"query"`
		Count   int    `json:"count"`
		Results []item `json:"results"`
	}{Query: q.Q, Results: []item{}}
	for _, h := range hits {
		out.Results = append(out.Results, item{
			Kind: string(h.Doc.Kind), Repo: h.Doc.Repo, ID: h.Doc.ID,
			Title: h.Doc.Title, Body: h.Doc.Body, Scope: h.Doc.Scope,
			Confidence: string(h.Doc.Confidence), Status: string(h.Doc.Status),
			At: h.Doc.At.Format(time.RFC3339), Score: h.Score,
		})
	}
	out.Count = len(out.Results)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ix := s.index()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"version":   version,
		"repos":     len(ix.Repos()),
		"documents": ix.Len(),
		"built_at":  ix.BuiltAt.Format(time.RFC3339),
	})
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s%s %s", r.Method, r.URL.Path, querySuffix(r), time.Since(start).Round(time.Microsecond))
	})
}

func querySuffix(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return ""
	}
	return "?" + r.URL.RawQuery
}

func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 60*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	default:
		return plural(int(d.Hours()/24/30), "month")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit + " ago"
	}
	return strconv.Itoa(n) + " " + unit + "s ago"
}
