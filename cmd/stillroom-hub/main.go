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
	"os"
	"strings"
	"time"

	"github.com/A4ever369/Stillroom/internal/pack"
)

var version = "dev"

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
	auth := NewAuth(os.Getenv("GITHUB_CLIENT_ID"), os.Getenv("GITHUB_CLIENT_SECRET"))

	hint := *installHint
	if hint == "" {
		hint = "curl -fsSL " + baseURL + "/install.sh | sh"
	}
	h := &hub{store: store, auth: auth, baseURL: baseURL, installHint: hint}
	if !auth.Enabled() {
		log.Println("sign-in is not configured (GITHUB_CLIENT_ID / GITHUB_CLIENT_SECRET)")
		log.Println("running in anonymous mode: packs will carry no publisher, and receivers are told so")
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
	mux.HandleFunc("GET /auth/callback", h.authCallback)
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
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	return logging(mux)
}

// ---- pages ----

type pageData struct {
	Account   *Account
	SignIn    bool
	BaseURL   string
	Command   string
	Version   string
	Record    *Record
	Pack      *pack.Pack
	Records   []Record
	PullCmd   string
	Anonymous bool
	Error     string
}

func (h *hub) page(r *http.Request) pageData {
	d := pageData{BaseURL: h.baseURL, Version: version, SignIn: h.auth.Enabled()}
	if acc, ok := h.auth.Current(r); ok {
		d.Account = &acc
	}
	d.Anonymous = !h.auth.Enabled()
	return d
}

func (h *hub) home(w http.ResponseWriter, r *http.Request) {
	d := h.page(r)
	// The text the whole product is built around. It is a prompt, not a shell
	// command — pasted into whichever agent the person already has open, so it
	// stays tool-agnostic. It explains itself, because the person pasting it
	// has never heard of any of this, and because what they are about to do is
	// give something of theirs to somebody else: that has to be a decision,
	// not a side effect.
	d.Command = "Stillroom — turn this session into something I can hand to a teammate.\n" +
		"\n" +
		"It distils this session on my machine, lists everything it would share, and\n" +
		"gives me a link only after I confirm. Whoever I send it to pastes that link\n" +
		"into their own agent and gets what I learned — no write-up, no meeting.\n" +
		"My transcript stays on this machine unless I ask for --full.\n" +
		"\n" +
		"Install if needed: " + h.installHint + "\n" +
		"Then run: still publish"
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
	d.PullCmd = h.pullInvitation(rec, id)
	render(w, "pack.html", d)
}

// pullInvitation is the text the receiver pastes. It names who sent it and
// what happens on arrival, because someone is about to let a stranger's
// material into their agent's context on the strength of a link.
func (h *hub) pullInvitation(rec Record, id string) string {
	who := rec.Publisher
	if who == "" {
		who = "Someone"
	}
	evidence := "Their transcripts were not included — only the conclusions."
	if rec.Mode == string(pack.ModeFull) {
		evidence = "They also chose to include their session transcripts, redacted, so I can see how they got there."
	}
	return "Stillroom — someone chose to send me what they learned.\n" +
		"\n" +
		who + " distilled this from their own sessions and sent me the link below.\n" +
		evidence + "\n" +
		"\n" +
		"Pulling it shows me what is inside before anything is written. It lands in\n" +
		".team-context/received/, attributed to them, and stays out of this project's\n" +
		"own facts — it is their report about their environment, not truth about mine.\n" +
		"\n" +
		"Install it if needed: " + h.installHint + "\n" +
		"Then run: still pull " + h.baseURL + "/k/" + id
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
	acc, signedIn := h.auth.Current(r)
	if h.auth.Enabled() && !signedIn {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "sign in first: run `still auth login`",
		})
		return
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
	if !h.auth.Enabled() {
		http.Error(w, "sign-in is not configured on this instance", http.StatusNotImplemented)
		return
	}
	next := r.URL.Query().Get("next")
	http.SetCookie(w, &http.Cookie{Name: "stillroom_next", Value: next, Path: "/", MaxAge: 600, HttpOnly: true})
	http.Redirect(w, r, h.auth.AuthorizeURL(h.baseURL, "s"), http.StatusSeeOther)
}

func (h *hub) authCallback(w http.ResponseWriter, r *http.Request) {
	acc, err := h.auth.Exchange(r.URL.Query().Get("code"), h.baseURL)
	if err != nil {
		http.Error(w, "sign-in failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	h.auth.SignIn(w, acc)
	next := "/me"
	if c, err := r.Cookie("stillroom_next"); err == nil && strings.HasPrefix(c.Value, "/") {
		next = c.Value
	}
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
