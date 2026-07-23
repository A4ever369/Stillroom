package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Identity comes from GitHub, never from us.
//
// The hub needs to answer one question — "who published this?" — because
// attribution is the receiver's only handle on whether to trust a pack. It
// does NOT need accounts, passwords or a user table, and it must never hold a
// credential it could leak. So sign-in delegates entirely to GitHub OAuth: the
// browser flow for the website, the device flow for the CLI (which has no
// browser to redirect and must never ask for a password at a prompt).

// Account is everything the hub knows about a person.
type Account struct {
	// ID is namespaced by provider ("github:123"), because two providers'
	// numeric ids are different people who would otherwise collide.
	ID       string `json:"id"`
	Login    string `json:"login"`  // display handle or name
	Avatar   string `json:"avatar"` // for the website
	Provider string `json:"provider"`
}

// Auth holds OAuth configuration plus the two short-lived in-memory tables:
// browser sessions and pending device authorizations. Both are ephemeral by
// design — losing them on restart signs people out, which is an acceptable
// cost for not persisting anything sensitive.
type Auth struct {
	providers []Provider
	mu        sync.Mutex
	sessions  map[string]Account // cookie value → account
	tokens    map[string]Account // CLI bearer token → account
	pending   map[string]*deviceReq
	// states holds in-flight CSRF states. An OAuth callback that cannot be
	// tied back to a sign-in this hub started is an attacker logging someone
	// into an account they control, so it is checked rather than assumed.
	states map[string]stateEntry
}

type stateEntry struct {
	provider string
	next     string
	expires  time.Time
}

type deviceReq struct {
	UserCode string
	Expires  time.Time
	Account  *Account // set once the browser side approves
	Token    string
}

func NewAuth(providers ...Provider) *Auth {
	return &Auth{
		providers: providers,
		sessions:  map[string]Account{},
		tokens:    map[string]Account{},
		pending:   map[string]*deviceReq{},
		states:    map[string]stateEntry{},
	}
}

// StartState mints a one-time CSRF state bound to the provider and the page to
// return to.
func (a *Auth) StartState(provider, next string) string {
	state := newSecret()
	a.mu.Lock()
	defer a.mu.Unlock()
	for s, e := range a.states { // opportunistic sweep; sign-ins are rare
		if time.Now().After(e.expires) {
			delete(a.states, s)
		}
	}
	a.states[state] = stateEntry{provider: provider, next: next, expires: time.Now().Add(10 * time.Minute)}
	return state
}

// TakeState consumes a state, returning the page to return to. Single use: a
// replayed callback must not sign anyone in twice.
func (a *Auth) TakeState(state, provider string) (next string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, found := a.states[state]
	if !found || e.provider != provider || time.Now().After(e.expires) {
		return "", false
	}
	delete(a.states, state)
	return e.next, true
}

// Enabled reports whether any provider is configured. Without one the hub runs
// in anonymous mode, which is fine for a local demo and stated plainly on the
// page — but packs then carry no publisher, and the receiver is told so.
func (a *Auth) Enabled() bool { return len(a.Providers()) > 0 }

func newSecret() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// A failure here means no entropy; refusing to mint a guessable
		// credential is the only safe response.
		panic("hub: no entropy for a session secret: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// userCode is what a person reads off their terminal and types into a browser.
// Deliberately short, unambiguous alphabet (no O/0, I/1), and short-lived.
func newUserCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("hub: no entropy for a device code: " + err.Error())
	}
	out := make([]byte, 0, 9)
	for i, x := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, alphabet[int(x)%len(alphabet)])
	}
	return string(out)
}

// ---- browser session ----

const sessionCookie = "stillroom_session"

func (a *Auth) SignIn(w http.ResponseWriter, acc Account) {
	sid := newSecret()
	a.mu.Lock()
	a.sessions[sid] = acc
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sid, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: true,
		MaxAge: int((30 * 24 * time.Hour).Seconds()),
	})
}

func (a *Auth) SignOut(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.mu.Lock()
		delete(a.sessions, c.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
}

// Current returns the signed-in account for a request, browser or CLI.
func (a *Auth) Current(r *http.Request) (Account, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		a.mu.Lock()
		acc, ok := a.tokens[strings.TrimPrefix(h, "Bearer ")]
		a.mu.Unlock()
		if ok {
			return acc, true
		}
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return Account{}, false
	}
	a.mu.Lock()
	acc, ok := a.sessions[c.Value]
	a.mu.Unlock()
	return acc, ok
}

// ---- device flow (the CLI) ----

// StartDevice mints a pending authorization. The CLI shows the user code and
// polls; the person approves in a browser they already trust.
func (a *Auth) StartDevice() (deviceCode, userCode string, expires time.Time) {
	deviceCode, userCode = newSecret(), newUserCode()
	expires = time.Now().Add(10 * time.Minute)
	a.mu.Lock()
	a.pending[deviceCode] = &deviceReq{UserCode: userCode, Expires: expires}
	a.mu.Unlock()
	return
}

// ApproveDevice binds a signed-in browser account to a user code.
func (a *Auth) ApproveDevice(userCode string, acc Account) error {
	userCode = strings.ToUpper(strings.TrimSpace(userCode))
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, req := range a.pending {
		if req.UserCode != userCode {
			continue
		}
		if time.Now().After(req.Expires) {
			return errors.New("that code has expired — run `still auth login` again")
		}
		tok := newSecret()
		req.Account, req.Token = &acc, tok
		a.tokens[tok] = acc
		return nil
	}
	return errors.New("no pending sign-in for that code")
}

// PollDevice is what the CLI calls. It returns an empty token while the
// request is still pending — not an error, because pending is the normal case.
func (a *Auth) PollDevice(deviceCode string) (token string, acc Account, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	req, ok := a.pending[deviceCode]
	if !ok {
		return "", Account{}, errors.New("unknown or completed device code")
	}
	if time.Now().After(req.Expires) {
		delete(a.pending, deviceCode)
		return "", Account{}, errors.New("sign-in expired")
	}
	if req.Account == nil {
		return "", Account{}, nil
	}
	token, acc = req.Token, *req.Account
	delete(a.pending, deviceCode)
	return token, acc, nil
}
