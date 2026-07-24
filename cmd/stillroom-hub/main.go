// Command stillroom-hub is the still room: the service that turns distilled
// knowledge into a link you can hand to another person.
//
// The premise is that explaining what you learned is expensive and lossy. You
// write a document nobody reads, or you paste a wall of context into chat.
// Instead: you distil the session you just finished, you get a link, and the
// person you send it to pastes that link into their own agent. They pick up
// everything you learned without you writing any of it down.
//
// Two properties this service must never lose:
//
//   - It only ever holds what a publisher explicitly chose to send, after
//     being shown it item by item. There is no background collection here.
//   - Everything it serves is untrusted input to whoever receives it. Packs
//     are handed on as attributed, quoted material — see internal/pack.
//
// Unlike stillroomd, this one does hold a source of truth: once a publisher's
// machine moves on, the hub's copy is the only copy.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/A4ever369/Stillroom/internal/pack"
)

// version is stamped by the release build. Binaries produced by `go install`
// get no ldflags, so fall back to the module version the toolchain embeds —
// otherwise every bug report from an installed binary says "dev".
var version = "dev"

func init() {
	if version != "dev" {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
}

//go:embed web
var webFS embed.FS

var staticFS = func() fs.FS {
	sub, err := fs.Sub(webFS, "web/static")
	if err != nil {
		panic(err)
	}
	return sub
}()

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"bytes": humanBytes,
	"ago":   humanAgo,
	"date":  func(t time.Time) string { return t.Format("2 Jan 2006") },
}).ParseFS(webFS, "web/*.html"))

// MaxPackBytes bounds an upload. A pack is meant to be read by the person
// receiving it; anything larger is not a pack, it is a dump.
const MaxPackBytes = 8 << 20

type hub struct {
	store *Store
	auth  *Auth
	rate  *rateLimiter
	// baseURL is what appears in the links people paste to each other, so it
	// must be the public address, not whatever the process happens to bind.
	baseURL string
	// installHint is the install line shown on both pages. Configurable so an
	// instance can point somewhere else, but it must never promise something
	// that does not exist: an install line that fails is the first thing a new
	// person tries and the first thing that breaks their trust.
	installHint string
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("stillroom-hub: ")

	addr := flag.String("addr", ":8080", "listen address")
	data := flag.String("data", "./hub-data", "directory for stored packs")
	base := flag.String("base-url", "", "public base URL (e.g. https://stillroom.sh); defaults to http://localhost<addr>")
	showVersion := flag.Bool("version", false, "print version and exit")
	installHint := flag.String("install-hint", "",
		"how the pages tell people to install `still` (default: curl the installer this hub serves)")
	flag.Parse()
	if *showVersion {
		fmt.Println("stillroom-hub", version)
		return
	}

	store, err := NewStore(*data)
	if err != nil {
		log.Fatal(err)
	}
	baseURL := strings.TrimRight(*base, "/")
	if baseURL == "" {
		baseURL = "http://localhost" + *addr
	}
	auth := NewAuth(githubProvider(), googleProvider())

	hint := *installHint
	if hint == "" {
		hint = "curl -fsSL " + baseURL + "/install.sh | sh"
	}
	h := &hub{store: store, auth: auth, rate: newRateLimiter(), baseURL: baseURL, installHint: hint}
	if names := auth.Providers(); len(names) == 0 {
		log.Println("sign-in is not configured — set GITHUB_CLIENT_ID/SECRET and/or GOOGLE_CLIENT_ID/SECRET")
		log.Println("running in anonymous mode: packs will carry no publisher, and receivers are told so")
	} else {
		for _, p := range names {
			log.Printf("sign-in enabled: %s (callback %s)", p.Label, p.redirectURI(baseURL))
		}
	}
	log.Printf("data in %s", *data)
	log.Printf("listening on %s (public %s)", *addr, baseURL)
	if err := http.ListenAndServe(*addr, h.routes()); err != nil {
		log.Fatal(err)
	}
}

func (h *hub) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.home)
	mux.HandleFunc("GET /k/{id}", h.viewPack)
	mux.HandleFunc("GET /k/{id}/raw", h.rawPack)
	mux.HandleFunc("POST /api/packs", h.createPack)
	mux.HandleFunc("GET /me", h.myPacks)
	mux.HandleFunc("POST /k/{id}/revoke", h.revokePack)
	mux.HandleFunc("POST /api/packs/{id}/revoke", h.revokePackAPI)

	mux.HandleFunc("GET /auth/start", h.authStart)
	mux.HandleFunc("GET /auth/callback/{provider}", h.authCallback)
	mux.HandleFunc("POST /auth/signout", h.authSignOut)
	mux.HandleFunc("GET /auth/device", h.deviceApprovePage)
	mux.HandleFunc("POST /auth/device", h.deviceApprove)
	mux.HandleFunc("POST /api/auth/device", h.deviceStart)
	mux.HandleFunc("POST /api/auth/device/poll", h.devicePoll)

	// Short enough to be typed and pasted. The same bytes are also under
	// /static/, but nobody wants "curl .../static/install.sh".
	mux.HandleFunc("GET /install.sh", func(w http.ResponseWriter, r *http.Request) {
		raw, err := webFS.ReadFile("web/static/install.sh")
		if err != nil {
			http.Error(w, "installer unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		// signin_enabled lets `still publish` decide whether to sign the user in
		// before uploading: on a hub with providers configured, publishing is
		// attributed by default so a pack shows up in the publisher's list; on a
		// hub without them, there is no one to attribute to and it stays
		// anonymous. The CLI reads this rather than guessing.
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "version": version,
			"signin_enabled": h.auth.Enabled(),
		})
	})
	// robots + sitemap: let the landing page be indexed, keep the per-pack /k/
	// pages out of search — a share link is meant for the person it was sent to,
	// not for a search engine to surface.
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "User-agent: *\nAllow: /$\nDisallow: /k/\nDisallow: /me\nDisallow: /auth/\nSitemap: %s/sitemap.xml\n", h.baseURL)
	})
	mux.HandleFunc("GET /sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+
			`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>%s/</loc></url></urlset>`+"\n", h.baseURL)
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	return logging(mux)
}

// ---- pages ----

type pageData struct {
	Title     string
	Desc      string
	Path      string
	Account   *Account
	SignIn    bool
	BaseURL   string
	Command   string
	Version   string
	Record    *Record
	Pack      *pack.Pack
	Records   []Record
	PullCmd   string
	ShareURL  string
	Providers []Provider
	Next      string
	Anonymous bool
	Error     string
}

func (h *hub) page(r *http.Request) pageData {
	d := pageData{BaseURL: h.baseURL, Version: version, SignIn: h.auth.Enabled(), Path: r.URL.Path}
	if acc, ok := h.auth.Current(r); ok {
		d.Account = &acc
	}
	d.Anonymous = !h.auth.Enabled()
	return d
}

func (h *hub) home(w http.ResponseWriter, r *http.Request) {
	d := h.page(r)
	d.Desc = "Stillroom distils a coding session with Claude Code or Codex into facts and hands them to a teammate as one link. No write-up, no meeting."
	// One line, because the thing being copied should be the thing that works —
	// the explaining belongs on the page, where it does not have to survive a
	// paste. Written as a request rather than a shell command so the agent
	// handles installation and PATH itself, which is exactly the part a
	// copy-pasted `curl ... && still publish` chain gets wrong.
	d.Command = h.publishInvitation()
	render(w, "home.html", d)
}

func (h *hub) viewPack(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, raw, err := h.store.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// An agent asking for this URL should get the pack, not the page. That is
	// what makes "paste the link into your agent" work without a second step.
	if wantsJSON(r) {
		h.store.CountDownload(id)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(raw)
		return
	}
	p, _, err := pack.Decode(raw)
	if err != nil {
		http.Error(w, "this pack is unreadable", http.StatusInternalServerError)
		return
	}
	d := h.page(r)
	d.Record, d.Pack = &rec, &p
	if rec.Note != "" {
		d.Title = rec.Note
	} else {
		d.Title = "A knowledge pack"
	}
	who := rec.Publisher
	if who == "" {
		who = "Someone"
	}
	d.Desc = fmt.Sprintf("%s shared %d facts from %s. Pull it into your own agent to pick up where they left off.",
		who, len(p.Facts), orDefault(rec.Repo, "a project"))
	d.PullCmd = h.pullInvitation(rec, id)
	// The bare link is what a publisher actually sends someone. It arrives in
	// a chat window, not a terminal, so the page has to hand it over as a link
	// and not only wrapped inside a command.
	d.ShareURL = h.baseURL + "/k/" + id
	render(w, "pack.html", d)
}

// publishInvitation is the one line a publisher copies. It is a request rather
// than a shell command so the agent resolves installation and PATH itself,
// which is exactly what a pasted `curl … && still publish` chain gets wrong.
func (h *hub) publishInvitation() string {
	return "Install Stillroom (" + h.installURL() +
		") and run `still publish` to turn this session into a link I can send someone."
}

// pullInvitation is the text the receiver pastes. It names who sent it and
// what happens on arrival, because someone is about to let a stranger's
// material into their agent's context on the strength of a link.
func (h *hub) pullInvitation(rec Record, id string) string {
	return "Install Stillroom (" + h.installURL() +
		") and run `still pull " + h.baseURL + "/k/" + id + "` to take in what they shared."
}

// installURL is the bare URL of the installer, for the one-line invitations.
// installHint is a full shell command; embedding it inside a sentence produced
// a line with a pipe in the middle that read like an instruction to the reader.
func (h *hub) installURL() string {
	if i := strings.Index(h.installHint, "http"); i >= 0 {
		rest := h.installHint[i:]
		if j := strings.IndexAny(rest, " |"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return h.baseURL + "/install.sh"
}

// rawPack is the explicit machine route. /k/{id} already returns the pack to
// anything asking for JSON; this exists so a link can be pointed at the bytes
// unambiguously, without depending on a client's Accept header.
func (h *hub) rawPack(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, raw, err := h.store.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.store.CountDownload(id)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(raw)
}

func (h *hub) createPack(w http.ResponseWriter, r *http.Request) {
	// Publishing never requires an account. Someone who pasted one line into
	// their agent should get a link out of it, not a sign-up form; signing in
	// buys attribution and a history, not permission. Anonymous callers get an
	// hourly budget instead — the open-endpoint risk is volume, not identity.
	acc, signedIn := h.auth.Current(r)
	if !signedIn {
		if ok, wait := h.rate.allow(clientIP(r), anonUploadsPerHour); !ok {
			w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())))
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": fmt.Sprintf(
					"too many anonymous publishes from here — try again in %d minutes, or run `still auth login` to lift the limit",
					int(wait.Minutes())+1),
			})
			return
		}
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, MaxPackBytes+1))
	if err != nil || len(raw) > MaxPackBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("a pack must be under %s", humanBytes(MaxPackBytes)),
		})
		return
	}
	p, bad, err := pack.Decode(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(p.Facts) == 0 && len(p.Playbooks) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the pack carries no knowledge"})
		return
	}

	// Publisher is stamped here, from the authenticated account — never taken
	// from the uploaded document. Attribution is the receiver's only trust
	// signal, so it must not be self-asserted.
	p.Publisher = acc.Login
	raw, err = p.Encode()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not re-encode the pack"})
		return
	}

	rec, revokeToken, err := h.store.Put(p, raw)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rec.PublisherID == "" && acc.ID != "" {
		rec.PublisherID = acc.ID
		_ = h.store.save(rec)
	}
	// The revoke token travels back exactly once, in this response. Sharing
	// works without an account, so for most publishers this is the only thing
	// that can take a link back — the CLI stores it locally and says so.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": rec.ID, "url": h.baseURL + "/k/" + rec.ID,
		"revoke_token": revokeToken,
		"dropped":      len(bad),
	})
}

func (h *hub) myPacks(w http.ResponseWriter, r *http.Request) {
	acc, ok := h.auth.Current(r)
	if !ok {
		http.Redirect(w, r, "/auth/start", http.StatusSeeOther)
		return
	}
	d := h.page(r)
	d.Records = h.store.ByPublisher(acc.ID)
	// The empty state is the most likely state for a new account, so it gets
	// the same line the landing page offers rather than a dead sentence.
	d.Command = h.publishInvitation()
	render(w, "me.html", d)
}

func (h *hub) revokePack(w http.ResponseWriter, r *http.Request) {
	acc, ok := h.auth.Current(r)
	if !ok {
		http.Error(w, "sign in first", http.StatusUnauthorized)
		return
	}
	if err := h.store.Revoke(r.PathValue("id"), acc.ID, ""); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	http.Redirect(w, r, "/me", http.StatusSeeOther)
}

// revokePackAPI is the CLI's route. It accepts either the revoke token handed
// out at publish time or a signed-in account, so someone who never created an
// account can still take back what they shared.
func (h *hub) revokePackAPI(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)

	var publisherID string
	if acc, ok := h.auth.Current(r); ok {
		publisherID = acc.ID
	}
	if err := h.store.Revoke(r.PathValue("id"), publisherID, body.Token); err != nil {
		// Deliberately one message for "no such pack" and "not yours": telling
		// them apart turns this endpoint into a way to enumerate what exists.
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "cannot revoke that link — wrong token, or it is not yours",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// ---- auth handlers ----

func (h *hub) authStart(w http.ResponseWriter, r *http.Request) {
	providers := h.auth.Providers()
	if len(providers) == 0 {
		http.Error(w, "sign-in is not configured on this instance", http.StatusNotImplemented)
		return
	}
	// Only accept a local path to return to; an open redirect on a sign-in
	// route is how people get walked to a lookalike site after authenticating.
	next := r.URL.Query().Get("next")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/me"
	}

	if name := r.URL.Query().Get("provider"); name != "" {
		p, ok := h.auth.Provider(name)
		if !ok {
			http.Error(w, "unknown sign-in provider", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, p.AuthorizeURL(h.baseURL, h.auth.StartState(p.Name, next)), http.StatusSeeOther)
		return
	}
	// One provider needs no choice; more than one gets a page.
	if len(providers) == 1 {
		p := providers[0]
		http.Redirect(w, r, p.AuthorizeURL(h.baseURL, h.auth.StartState(p.Name, next)), http.StatusSeeOther)
		return
	}
	d := h.page(r)
	d.Providers = providers
	d.Next = next
	render(w, "signin.html", d)
}

func (h *hub) authCallback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	p, ok := h.auth.Provider(name)
	if !ok {
		http.Error(w, "unknown sign-in provider", http.StatusBadRequest)
		return
	}
	// A callback that cannot be tied to a sign-in this hub started is either a
	// stale tab or someone trying to log a visitor into an account they own.
	next, ok := h.auth.TakeState(r.URL.Query().Get("state"), name)
	if !ok {
		http.Error(w, "this sign-in link has expired — start again", http.StatusBadRequest)
		return
	}
	acc, err := p.Exchange(r.URL.Query().Get("code"), h.baseURL)
	if err != nil {
		http.Error(w, "sign-in failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	h.auth.SignIn(w, acc)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (h *hub) authSignOut(w http.ResponseWriter, r *http.Request) {
	h.auth.SignOut(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *hub) deviceStart(w http.ResponseWriter, r *http.Request) {
	if !h.auth.Enabled() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "sign-in is not configured on this instance"})
		return
	}
	deviceCode, userCode, expires := h.auth.StartDevice()
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code": deviceCode,
		"user_code":   userCode,
		"verify_url":  h.baseURL + "/auth/device",
		"expires_at":  expires.UTC().Format(time.RFC3339),
	})
}

func (h *hub) devicePoll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceCode string `json:"device_code"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	token, acc, err := h.auth.PollDevice(body.DeviceCode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"pending": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "login": acc.Login})
}

func (h *hub) deviceApprovePage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.Current(r); !ok {
		http.Redirect(w, r, "/auth/start?next=/auth/device", http.StatusSeeOther)
		return
	}
	render(w, "device.html", h.page(r))
}

func (h *hub) deviceApprove(w http.ResponseWriter, r *http.Request) {
	acc, ok := h.auth.Current(r)
	if !ok {
		http.Redirect(w, r, "/auth/start?next=/auth/device", http.StatusSeeOther)
		return
	}
	d := h.page(r)
	if err := h.auth.ApproveDevice(r.FormValue("code"), acc); err != nil {
		d.Error = err.Error()
		render(w, "device.html", d)
		return
	}
	render(w, "device-ok.html", d)
}

// ---- plumbing ----

func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
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
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func humanBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.0f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d d ago", int(d.Hours()/24))
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
