package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/A4ever369/Stillroom/internal/ir"
	"github.com/A4ever369/Stillroom/internal/pack"
)

func testHub(t *testing.T, signIn bool) *hub {
	t.Helper()
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := NewAuth()
	if signIn {
		a = NewAuth(testProvider("github"))
	}
	return &hub{store: st, auth: a, baseURL: "https://example.test"}
}

func samplePack(t *testing.T) (pack.Pack, []byte) {
	t.Helper()
	s := ir.Store{Root: t.TempDir()}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	err := s.WriteFact(ir.Fact{
		ID: "deploy.db", Scope: "repo:acme", ObservedAt: time.Now().UTC().Truncate(time.Second),
		Source: "claude-code://x", Confidence: ir.ConfidenceHigh, Status: ir.StatusActive,
		Body: "Production is reached through pgbouncer on 6432.",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := pack.Build(s, nil, pack.ModeKnowledge, "a note", pack.Origin{Repo: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return p, raw
}

func do(h *hub, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.routes().ServeHTTP(w, req)
	return w
}

func post(t *testing.T, h *hub, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	return do(h, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body))))
}

// The whole product is "paste the link into your agent". That only works
// without a second step if one URL serves the page to a browser and the pack
// to anything asking for JSON.
func TestOneLinkServesBothPageAndPack(t *testing.T) {
	h := testHub(t, false)
	_, raw := samplePack(t)
	w := post(t, h, "/api/packs", raw)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	var created struct{ URL, ID string }
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	agent := httptest.NewRequest(http.MethodGet, "/k/"+created.ID, nil)
	agent.Header.Set("Accept", "application/json")
	got := do(h, agent)
	if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("an agent should get the pack, got %s", ct)
	}
	if !strings.Contains(got.Body.String(), "pgbouncer") {
		t.Error("the pack body did not come back")
	}

	browser := httptest.NewRequest(http.MethodGet, "/k/"+created.ID, nil)
	browser.Header.Set("Accept", "text/html")
	page := do(h, browser)
	if !strings.Contains(page.Body.String(), "still pull https://example.test/k/"+created.ID) {
		t.Error("the page should show the command to copy")
	}
}

// Publishing twice from the same knowledge must not litter the hub with
// duplicate links — and must not mint a second revoke token, which would
// silently invalidate the one the publisher already has.
func TestUploadIsIdempotent(t *testing.T) {
	h := testHub(t, false)
	_, raw := samplePack(t)

	var first, second struct {
		URL         string `json:"url"`
		RevokeToken string `json:"revoke_token"`
	}
	if err := json.Unmarshal(post(t, h, "/api/packs", raw).Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(post(t, h, "/api/packs", raw).Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if first.URL != second.URL {
		t.Errorf("same content produced different links: %s vs %s", first.URL, second.URL)
	}
	if first.RevokeToken == "" {
		t.Error("the first publish must hand back a revoke token")
	}
	if second.RevokeToken != "" {
		t.Error("re-publishing must not issue a second token")
	}
}

// The CLI route accepts the token alone, so someone who never made an account
// can still take a link back — and it must not leak whether a link exists.
func TestRevokeViaAPIWithTokenOnly(t *testing.T) {
	h := testHub(t, false)
	_, raw := samplePack(t)
	var created struct {
		ID          string `json:"id"`
		RevokeToken string `json:"revoke_token"`
	}
	if err := json.Unmarshal(post(t, h, "/api/packs", raw).Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	bad := post(t, h, "/api/packs/"+created.ID+"/revoke", []byte(`{"token":"nope"}`))
	missing := post(t, h, "/api/packs/aaaaaaaaaaaa/revoke", []byte(`{"token":"nope"}`))
	if bad.Code != http.StatusForbidden || missing.Code != http.StatusForbidden {
		t.Errorf("both should be 403: wrong-token=%d no-such-pack=%d", bad.Code, missing.Code)
	}
	if bad.Body.String() != missing.Body.String() {
		t.Error("the responses must not reveal whether a link exists")
	}

	ok := post(t, h, "/api/packs/"+created.ID+"/revoke",
		[]byte(`{"token":"`+created.RevokeToken+`"}`))
	if ok.Code != http.StatusOK {
		t.Fatalf("revoke with the right token: %d %s", ok.Code, ok.Body)
	}
	if w := do(h, httptest.NewRequest(http.MethodGet, "/k/"+created.ID, nil)); w.Code != http.StatusNotFound {
		t.Errorf("the link should be gone, got %d", w.Code)
	}
}

// Attribution is the receiver's only trust signal, so it cannot be claimed by
// the uploader — with sign-in configured, an unauthenticated upload is refused
// outright rather than stored unattributed.
func TestPublisherCannotBeSelfAssertedAndAuthIsRequiredWhenConfigured(t *testing.T) {
	h := testHub(t, true)
	p, _ := samplePack(t)
	p.Publisher = "someone-important"
	raw, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if w := post(t, h, "/api/packs", raw); w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without sign-in, got %d", w.Code)
	}

	// Anonymous mode (no OAuth configured) accepts it, but strips the claim.
	anon := testHub(t, false)
	w := post(t, anon, "/api/packs", raw)
	if w.Code != http.StatusCreated {
		t.Fatalf("anonymous upload: %d %s", w.Code, w.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	rec, _, err := anon.store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Publisher != "" {
		t.Errorf("a self-asserted publisher survived: %q", rec.Publisher)
	}
}

func TestUploadRejectsJunk(t *testing.T) {
	h := testHub(t, false)
	for name, body := range map[string]string{
		"not json":        "hello",
		"no knowledge":    `{"version":1,"mode":"knowledge","origin":{},"facts":[],"playbooks":[]}`,
		"from the future": `{"version":99,"mode":"knowledge","origin":{}}`,
	} {
		if w := post(t, h, "/api/packs", []byte(body)); w.Code < 400 {
			t.Errorf("%s: want a rejection, got %d", name, w.Code)
		}
	}
}

// Revocation and expiry both make a link stop resolving, and a caller must not
// be able to tell "taken back" from "never existed".
func TestRevokedAndExpiredPacksAreGone(t *testing.T) {
	h := testHub(t, false)
	p, raw := samplePack(t)
	rec, revokeToken, err := h.store.Put(p, raw)
	if err != nil {
		t.Fatal(err)
	}
	rec.PublisherID = "u1"
	if err := h.store.save(rec); err != nil {
		t.Fatal(err)
	}

	if err := h.store.Revoke(rec.ID, "someone-else", ""); err == nil {
		t.Error("another account must not be able to revoke it")
	}
	if err := h.store.Revoke(rec.ID, "", "wrong-token"); err == nil {
		t.Error("a wrong revoke token must not work")
	}
	// The token issued at publish time is the anonymous publisher's only way
	// back, so it must work on its own.
	if err := h.store.Revoke(rec.ID, "", revokeToken); err != nil {
		t.Fatalf("the revoke token should work without an account: %v", err)
	}
	if _, _, err := h.store.Get(rec.ID); !os.IsNotExist(err) {
		t.Errorf("a revoked pack must read as gone, got %v", err)
	}
	if w := do(h, httptest.NewRequest(http.MethodGet, "/k/"+rec.ID, nil)); w.Code != http.StatusNotFound {
		t.Errorf("want 404 for a revoked link, got %d", w.Code)
	}

	// Expiry, the other way a publisher takes something back.
	p2, raw2 := samplePack(t)
	p2.Note = "expiring"
	raw2, _ = p2.Encode()
	rec2, _, _ := h.store.Put(p2, raw2)
	rec2.ExpiresAt = time.Now().Add(-time.Minute)
	_ = h.store.save(rec2)
	if _, _, err := h.store.Get(rec2.ID); !os.IsNotExist(err) {
		t.Errorf("an expired pack must read as gone, got %v", err)
	}
}

// Every filesystem path is built from a URL segment, so traversal attempts
// must die at the door.
func TestStoreRejectsPathTraversalIDs(t *testing.T) {
	h := testHub(t, false)
	for _, id := range []string{"../../etc/passwd", "..", "a/b", "AB12", "zz", ""} {
		if _, _, err := h.store.Get(id); err == nil {
			t.Errorf("id %q was accepted", id)
		}
	}
}

// The device flow is what lets a terminal sign in without ever handling a
// password. Pending is the normal state, not an error.
func TestDeviceFlow(t *testing.T) {
	a := NewAuth(testProvider("github"))
	deviceCode, userCode, _ := a.StartDevice()

	tok, _, err := a.PollDevice(deviceCode)
	if err != nil || tok != "" {
		t.Fatalf("before approval want pending, got token=%q err=%v", tok, err)
	}
	if err := a.ApproveDevice("NOPE-NOPE", Account{Login: "x"}); err == nil {
		t.Error("an unknown code must not approve anything")
	}
	if err := a.ApproveDevice(strings.ToLower(userCode), Account{ID: "1", Login: "allen"}); err != nil {
		t.Fatalf("approval should be case-insensitive: %v", err)
	}

	tok, acc, err := a.PollDevice(deviceCode)
	if err != nil || tok == "" || acc.Login != "allen" {
		t.Fatalf("after approval: tok=%q acc=%+v err=%v", tok, acc, err)
	}
	// The device code is single-use.
	if _, _, err := a.PollDevice(deviceCode); err == nil {
		t.Error("a spent device code must not poll again")
	}

	// The minted token authenticates a request.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	if got, ok := a.Current(req); !ok || got.Login != "allen" {
		t.Errorf("token did not authenticate: %+v %v", got, ok)
	}
}

func TestDeviceCodesExpire(t *testing.T) {
	a := NewAuth(testProvider("github"))
	deviceCode, userCode, _ := a.StartDevice()
	a.pending[deviceCode].Expires = time.Now().Add(-time.Second)

	if err := a.ApproveDevice(userCode, Account{Login: "allen"}); err == nil {
		t.Error("an expired code must not be approvable")
	}
	if _, _, err := a.PollDevice(deviceCode); err == nil {
		t.Error("an expired code must not poll to success")
	}
}

func TestUnauthenticatedCannotRevokeOrListPacks(t *testing.T) {
	h := testHub(t, true)
	if w := do(h, httptest.NewRequest(http.MethodPost, "/k/abc123/revoke", nil)); w.Code != http.StatusUnauthorized {
		t.Errorf("revoke without sign-in: %d", w.Code)
	}
	if w := do(h, httptest.NewRequest(http.MethodGet, "/me", nil)); w.Code != http.StatusSeeOther {
		t.Errorf("/me should send you to sign in, got %d", w.Code)
	}
}

// testProvider is a configured provider that never talks to the network — the
// flows under test are session, device and state handling, not OAuth round
// trips.
func testProvider(name string) Provider {
	p := githubProvider()
	p.Name, p.ClientID, p.ClientSecret = name, "id", "secret"
	return p
}

// A callback that cannot be tied to a sign-in this hub started is either a
// stale tab or someone trying to log a visitor into an account they control.
func TestOAuthStateIsSingleUseAndProviderBound(t *testing.T) {
	a := NewAuth(testProvider("github"), testProvider("google"))
	state := a.StartState("github", "/me")

	if _, ok := a.TakeState(state, "google"); ok {
		t.Error("a state minted for one provider must not work for another")
	}
	if _, ok := a.TakeState("never-issued", "github"); ok {
		t.Error("an unknown state must be refused")
	}
	next, ok := a.TakeState(state, "github")
	if !ok || next != "/me" {
		t.Fatalf("valid state should return its destination: %q %v", next, ok)
	}
	if _, ok := a.TakeState(state, "github"); ok {
		t.Error("a state must be single use")
	}
}

// The sign-in route must not become an open redirect.
func TestSignInRefusesOffsiteReturnPaths(t *testing.T) {
	h := testHub(t, true)
	for _, next := range []string{"https://evil.test/x", "//evil.test/x"} {
		req := httptest.NewRequest(http.MethodGet, "/auth/start?provider=github&next="+url.QueryEscape(next), nil)
		w := do(h, req)
		if loc := w.Header().Get("Location"); strings.Contains(loc, "evil.test") {
			t.Errorf("next=%q leaked into the redirect: %s", next, loc)
		}
	}
}

// Two providers means the visitor picks; one means no pointless page.
func TestSignInPageAppearsOnlyWithAChoice(t *testing.T) {
	two := &hub{store: testHub(t, false).store, auth: NewAuth(testProvider("github"), testProvider("google")), baseURL: "https://example.test"}
	w := do(two, httptest.NewRequest(http.MethodGet, "/auth/start", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Continue with") {
		t.Errorf("two providers should render a chooser, got %d", w.Code)
	}
	one := &hub{store: two.store, auth: NewAuth(testProvider("github")), baseURL: "https://example.test"}
	w = do(one, httptest.NewRequest(http.MethodGet, "/auth/start", nil))
	if w.Code != http.StatusSeeOther {
		t.Errorf("one provider should redirect straight out, got %d", w.Code)
	}
}
