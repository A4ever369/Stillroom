package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/A4ever369/Stillroom/internal/index"
	"github.com/A4ever369/Stillroom/internal/ir"
)

// knowledgeRepo creates a repo root holding one fact and one playbook.
func knowledgeRepo(t *testing.T, dir, factID, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := ir.Store{Root: dir}
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	err := st.WriteFact(ir.Fact{
		ID: factID, Scope: "repo:demo", ObservedAt: time.Now().Add(-time.Hour),
		Source: "claude-code://abc", Confidence: ir.ConfidenceHigh,
		Status: ir.StatusActive, Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func newTestServer(t *testing.T, repos ...index.Repo) *server {
	t.Helper()
	s := &server{}
	s.rebuild(repos)
	return s
}

func get(t *testing.T, s *server, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// -scan walks a tree and picks up exactly the directories holding a knowledge
// base — the zero-config path for "index everything I have checked out".
func TestScanFindsKnowledgeRepos(t *testing.T) {
	root := t.TempDir()
	knowledgeRepo(t, filepath.Join(root, "infra"), "deploy.host", "The box is at 10.0.0.9.")
	knowledgeRepo(t, filepath.Join(root, "team", "web"), "ci.node", "CI pins Node 22.")
	if err := os.MkdirAll(filepath.Join(root, "not-a-repo", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Heavy and hidden directories must be skipped, not descended into.
	knowledgeRepo(t, filepath.Join(root, "web", "node_modules", "pkg"), "junk.fact", "Should be skipped.")

	found, err := scanRepos(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range found {
		names = append(names, r.Name)
	}
	want := "infra team/web"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("scan found %q, want %q", got, want)
	}
}

// An explicitly named path without a knowledge base is a user error worth
// reporting — unlike a scan, where a non-match is simply not a match.
func TestResolveReposRejectsPathWithoutStore(t *testing.T) {
	_, err := resolveRepos(multiFlag{t.TempDir()}, nil)
	if err == nil {
		t.Fatal("want an error for a path with no .team-context/")
	}
	if !strings.Contains(err.Error(), "still init") {
		t.Errorf("the error should tell the user what to do, got: %v", err)
	}
}

// The same repo reached two ways is indexed once, and names are honored.
func TestResolveReposDedupesAndNames(t *testing.T) {
	root := t.TempDir()
	dir := knowledgeRepo(t, filepath.Join(root, "infra"), "deploy.host", "The box is at 10.0.0.9.")

	repos, err := resolveRepos(multiFlag{"acme/infra=" + dir}, multiFlag{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("want 1 deduped repo, got %+v", repos)
	}
	if repos[0].Name != "acme/infra" {
		t.Errorf("the explicit name should win, got %q", repos[0].Name)
	}
}

func TestSearchPageRendersHits(t *testing.T) {
	dir := knowledgeRepo(t, filepath.Join(t.TempDir(), "infra"), "deploy.host", "The box is at 10.0.0.9.")
	s := newTestServer(t, index.Repo{Name: "acme/infra", Path: dir})

	w := get(t, s, "/?q=box")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"deploy.host", "1 result for", "acme/infra"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

// A query with no answer must say so honestly rather than showing noise.
func TestSearchPageEmptyState(t *testing.T) {
	dir := knowledgeRepo(t, filepath.Join(t.TempDir(), "infra"), "deploy.host", "The box is at 10.0.0.9.")
	s := newTestServer(t, index.Repo{Name: "acme/infra", Path: dir})

	if body := get(t, s, "/?q=zzzzznothing").Body.String(); !strings.Contains(body, "Nothing matched") {
		t.Errorf("want an explicit empty state:\n%s", body)
	}
}

// Facet links must preserve the query instead of silently resetting it.
func TestFacetLinksPreserveTheQuery(t *testing.T) {
	dir := knowledgeRepo(t, filepath.Join(t.TempDir(), "infra"), "deploy.host", "The box is at 10.0.0.9.")
	s := newTestServer(t, index.Repo{Name: "acme/infra", Path: dir})

	body := get(t, s, "/?q=box&kind=fact").Body.String()
	if !strings.Contains(body, "kind=fact&amp;q=box") && !strings.Contains(body, "q=box&amp;kind=fact") {
		t.Errorf("facet links dropped the query:\n%s", body)
	}
}

func TestDocPageAndNotFound(t *testing.T) {
	dir := knowledgeRepo(t, filepath.Join(t.TempDir(), "infra"), "deploy.host", "The box is at 10.0.0.9.")
	s := newTestServer(t, index.Repo{Name: "acme/infra", Path: dir})

	w := get(t, s, "/d?repo=acme/infra&kind=fact&id=deploy.host")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"deploy.host", "10.0.0.9", ".team-context/facts/deploy.host.md"} {
		if !strings.Contains(body, want) {
			t.Errorf("doc page missing %q", want)
		}
	}
	// The privacy boundary is stated on the page, not just in the docs.
	if !strings.Contains(body, "never leave that machine") {
		t.Error("doc page should state the evidence boundary")
	}

	if w := get(t, s, "/d?repo=acme/infra&kind=fact&id=nope"); w.Code != http.StatusNotFound {
		t.Errorf("want 404 for an unknown document, got %d", w.Code)
	}
}

func TestAPISearchReturnsJSON(t *testing.T) {
	dir := knowledgeRepo(t, filepath.Join(t.TempDir(), "infra"), "deploy.host", "The box is at 10.0.0.9.")
	s := newTestServer(t, index.Repo{Name: "acme/infra", Path: dir})

	w := get(t, s, "/api/search?q=box")
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type %q", ct)
	}
	var out struct {
		Query   string `json:"query"`
		Count   int    `json:"count"`
		Results []struct {
			ID   string `json:"id"`
			Repo string `json:"repo"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json: %v\n%s", err, w.Body.String())
	}
	if out.Count != 1 || out.Results[0].ID != "deploy.host" || out.Results[0].Repo != "acme/infra" {
		t.Errorf("unexpected payload: %+v", out)
	}
}

// An empty result set must serialize as [] rather than null, so clients can
// iterate without a nil check.
func TestAPISearchEmptyIsArray(t *testing.T) {
	dir := knowledgeRepo(t, filepath.Join(t.TempDir(), "infra"), "deploy.host", "The box is at 10.0.0.9.")
	s := newTestServer(t, index.Repo{Name: "acme/infra", Path: dir})

	if body := get(t, s, "/api/search?q=zzzzznothing").Body.String(); !strings.Contains(body, `"results": []`) {
		t.Errorf("want an empty array:\n%s", body)
	}
}

func TestHealthz(t *testing.T) {
	dir := knowledgeRepo(t, filepath.Join(t.TempDir(), "infra"), "deploy.host", "The box is at 10.0.0.9.")
	s := newTestServer(t, index.Repo{Name: "acme/infra", Path: dir})

	var out map[string]any
	if err := json.Unmarshal(get(t, s, "/healthz").Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true || out["documents"] != float64(1) {
		t.Errorf("unexpected healthz: %v", out)
	}
}

// The whole UI ships inside the binary: no CDN, no asset volume, no network
// egress from the container to render a page.
func TestStaticAssetsAreEmbedded(t *testing.T) {
	s := newTestServer(t)
	w := get(t, s, "/static/style.css")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "--canvas") {
		t.Fatalf("stylesheet not served from the binary: %d", w.Code)
	}
}

// Rebuilding swaps a whole snapshot in: knowledge added on disk appears after
// a refresh, and readers never observe a partially-built index.
func TestRebuildPicksUpNewKnowledge(t *testing.T) {
	dir := knowledgeRepo(t, filepath.Join(t.TempDir(), "infra"), "deploy.host", "The box is at 10.0.0.9.")
	repos := []index.Repo{{Name: "acme/infra", Path: dir}}
	s := newTestServer(t, repos...)
	if s.index().Len() != 1 {
		t.Fatalf("want 1 doc, got %d", s.index().Len())
	}

	st := ir.Store{Root: dir}
	err := st.WriteFact(ir.Fact{
		ID: "ci.node", Scope: "repo:demo", ObservedAt: time.Now(),
		Source: "claude-code://def", Confidence: ir.ConfidenceHigh,
		Status: ir.StatusActive, Body: "CI pins Node 22.",
	})
	if err != nil {
		t.Fatal(err)
	}

	s.rebuild(repos)
	if s.index().Len() != 2 {
		t.Fatalf("want 2 docs after rebuild, got %d", s.index().Len())
	}
}

// The server must never expose the evidence plane. Queue entries are machine-
// private state inside .team-context/ — indexing them would leak transcript
// paths into an org-wide search box.
func TestQueueIsNeverIndexed(t *testing.T) {
	dir := knowledgeRepo(t, filepath.Join(t.TempDir(), "infra"), "deploy.host", "The box is at 10.0.0.9.")
	queued := filepath.Join(dir, ir.DirName, "queue", "session.md")
	if err := os.WriteFile(queued, []byte("secretpathmarker /Users/someone/.claude/x.jsonl"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, index.Repo{Name: "acme/infra", Path: dir})

	if body := get(t, s, "/?q=secretpathmarker").Body.String(); strings.Contains(body, "secretpathmarker /Users") {
		t.Error("queue contents leaked into search results")
	}
}
