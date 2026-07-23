package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// Sign-in delegates to identity providers people already have, and the hub
// never sees a password.
//
// Two are supported because the audience splits cleanly: developers have
// GitHub, everyone has Google. Adding a third is a Provider literal, not a
// change to any flow.
//
// The access token a provider hands back is used exactly once, to read the
// profile, and then dropped. The hub has no business holding a credential
// that can act on someone's account elsewhere.

// Provider is one OAuth identity source.
type Provider struct {
	Name  string // url-safe key, e.g. "github"
	Label string // what a person reads on the button
	// Configuration, from the environment.
	ClientID     string
	ClientSecret string

	authURL  string
	tokenURL string
	userURL  string
	scope    string
	// parse turns the provider's profile JSON into an Account. Each provider
	// names things differently and none of them agree on what a display name
	// is, so this is per-provider rather than a shared struct with every
	// possible field on it.
	parse func([]byte) (Account, error)
}

// Configured reports whether this provider has credentials.
func (p Provider) Configured() bool { return p.ClientID != "" && p.ClientSecret != "" }

func (p Provider) redirectURI(base string) string { return base + "/auth/callback/" + p.Name }

// AuthorizeURL is where the browser is sent to approve the sign-in.
func (p Provider) AuthorizeURL(base, state string) string {
	v := url.Values{}
	v.Set("client_id", p.ClientID)
	v.Set("redirect_uri", p.redirectURI(base))
	v.Set("scope", p.scope)
	v.Set("state", state)
	v.Set("response_type", "code") // ignored by GitHub, required by Google
	return p.authURL + "?" + v.Encode()
}

// Exchange swaps the callback code for an account.
func (p Provider) Exchange(code, base string) (Account, error) {
	form := url.Values{
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"code":          {code},
		"redirect_uri":  {p.redirectURI(base)},
		"grant_type":    {"authorization_code"}, // required by Google
	}
	req, _ := http.NewRequest(http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Account{}, err
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return Account{}, err
	}
	if tok.AccessToken == "" {
		reason := tok.ErrorDesc
		if reason == "" {
			reason = tok.Error
		}
		return Account{}, fmt.Errorf("%s declined the sign-in: %s", p.Label, reason)
	}

	ureq, _ := http.NewRequest(http.MethodGet, p.userURL, nil)
	ureq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	ureq.Header.Set("Accept", "application/json")
	uresp, err := client.Do(ureq)
	if err != nil {
		return Account{}, err
	}
	defer uresp.Body.Close()
	raw := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := uresp.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if err != nil || len(raw) > 1<<20 {
			break
		}
	}
	acc, err := p.parse(raw)
	if err != nil {
		return Account{}, err
	}
	if acc.Login == "" {
		return Account{}, errors.New(p.Label + " returned an account with no name")
	}
	// Namespaced, because a GitHub id and a Google id are different people who
	// may well collide as bare numbers.
	acc.ID = p.Name + ":" + acc.ID
	acc.Provider = p.Name
	return acc, nil
}

func githubProvider() Provider {
	return Provider{
		Name: "github", Label: "GitHub",
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		authURL:      "https://github.com/login/oauth/authorize",
		tokenURL:     "https://github.com/login/oauth/access_token",
		userURL:      "https://api.github.com/user",
		scope:        "read:user",
		parse: func(raw []byte) (Account, error) {
			var u struct {
				ID     int64  `json:"id"`
				Login  string `json:"login"`
				Avatar string `json:"avatar_url"`
			}
			if err := json.Unmarshal(raw, &u); err != nil {
				return Account{}, err
			}
			return Account{ID: fmt.Sprint(u.ID), Login: u.Login, Avatar: u.Avatar}, nil
		},
	}
}

func googleProvider() Provider {
	return Provider{
		Name: "google", Label: "Google",
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		authURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		tokenURL:     "https://oauth2.googleapis.com/token",
		userURL:      "https://openidconnect.googleapis.com/v1/userinfo",
		scope:        "openid profile email",
		parse: func(raw []byte) (Account, error) {
			var u struct {
				Sub     string `json:"sub"`
				Name    string `json:"name"`
				Email   string `json:"email"`
				Picture string `json:"picture"`
			}
			if err := json.Unmarshal(raw, &u); err != nil {
				return Account{}, err
			}
			// Google has no handle. A display name is what a receiver will read
			// as the publisher, so prefer it; fall back to the local part of the
			// address rather than publishing somebody's full email address.
			name := strings.TrimSpace(u.Name)
			if name == "" {
				name, _, _ = strings.Cut(u.Email, "@")
			}
			return Account{ID: u.Sub, Login: name, Avatar: u.Picture}, nil
		},
	}
}

// Providers returns the configured providers, in a stable order.
func (a *Auth) Providers() []Provider {
	var out []Provider
	for _, p := range a.providers {
		if p.Configured() {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Provider looks one up by name.
func (a *Auth) Provider(name string) (Provider, bool) {
	for _, p := range a.providers {
		if p.Name == name && p.Configured() {
			return p, true
		}
	}
	return Provider{}, false
}
