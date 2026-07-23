package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Signing in from a terminal, without ever handling a password.
//
// The CLI has no browser to redirect and must never prompt for a credential —
// a tool that asks for your password at a shell prompt is teaching a habit
// that gets people phished. So it uses the device flow: the terminal shows a
// short code, you approve it in a browser you already trust, and the hub hands
// back a token scoped to this machine.
//
// Signing in is optional. Publishing anonymously works; signing in is what
// puts your name on what you publish, and attribution is the only handle the
// person receiving a pack has on whether to trust it.

// credentials is the on-disk token store, keyed by hub so one machine can talk
// to a company's self-hosted hub and the public one without them colliding.
type credentials struct {
	Hubs map[string]hubCredential `json:"hubs"`
}

type hubCredential struct {
	Token string `json:"token"`
	Login string `json:"login"`
	Since string `json:"since"`
}

func credentialsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "stillroom", "credentials.json"), nil
}

func loadCredentials() credentials {
	c := credentials{Hubs: map[string]hubCredential{}}
	path, err := credentialsPath()
	if err != nil {
		return c
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	// A corrupt credentials file must not brick the CLI: fall back to
	// signed-out rather than failing every command.
	if err := json.Unmarshal(raw, &c); err != nil || c.Hubs == nil {
		return credentials{Hubs: map[string]hubCredential{}}
	}
	return c
}

func saveCredentials(c credentials) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// 0600, and written through a temp file so an interrupted write cannot
	// leave a half-file that reads as signed-out with a stale token in it.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// authToken returns the token for the active hub. The environment wins, so a
// CI job can pass one without touching the user's config.
func authToken() string {
	if v := os.Getenv("STILLROOM_TOKEN"); v != "" {
		return v
	}
	return loadCredentials().Hubs[hubBase()].Token
}

func cmdAuth(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: still auth login | status | logout")
	}
	switch args[0] {
	case "login":
		return authLogin()
	case "status":
		return authStatus()
	case "logout":
		return authLogout()
	default:
		return fmt.Errorf("unknown: still auth %s", args[0])
	}
}

func authStatus() error {
	hub := hubBase()
	if os.Getenv("STILLROOM_TOKEN") != "" {
		fmt.Printf("signed in to %s via STILLROOM_TOKEN\n", hub)
		return nil
	}
	cred, ok := loadCredentials().Hubs[hub]
	if !ok || cred.Token == "" {
		fmt.Printf("not signed in to %s\n", hub)
		fmt.Println("run `still auth login` — publishing works without it, but your packs")
		fmt.Println("will arrive unattributed, and the receiver is told so.")
		return nil
	}
	fmt.Printf("signed in to %s as %s (since %s)\n", hub, cred.Login, cred.Since)
	return nil
}

func authLogout() error {
	hub := hubBase()
	c := loadCredentials()
	if _, ok := c.Hubs[hub]; !ok {
		fmt.Printf("not signed in to %s\n", hub)
		return nil
	}
	delete(c.Hubs, hub)
	if err := saveCredentials(c); err != nil {
		return err
	}
	fmt.Printf("signed out of %s\n", hub)
	return nil
}

type deviceStart struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	VerifyURL  string `json:"verify_url"`
	ExpiresAt  string `json:"expires_at"`
	Error      string `json:"error"`
}

type devicePoll struct {
	Token   string `json:"token"`
	Login   string `json:"login"`
	Pending bool   `json:"pending"`
	Error   string `json:"error"`
}

func authLogin() error {
	hub := hubBase()

	var start deviceStart
	if err := postJSON(hub+"/api/auth/device", nil, &start); err != nil {
		return err
	}
	if start.Error != "" {
		return errors.New(start.Error)
	}
	if start.UserCode == "" {
		return errors.New("the hub did not start a sign-in")
	}

	fmt.Printf("\n  Your code:  %s\n\n", start.UserCode)
	fmt.Printf("  Open %s and enter it.\n", start.VerifyURL)
	openBrowser(start.VerifyURL)
	fmt.Print("\n  waiting")

	// Poll until approved or the code expires. A slow interval on purpose:
	// this is a human typing a code in another window, not a race.
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		fmt.Print(".")

		var poll devicePoll
		if err := postJSON(hub+"/api/auth/device/poll",
			map[string]string{"device_code": start.DeviceCode}, &poll); err != nil {
			// A transient network blip must not end a sign-in the user is
			// halfway through completing in their browser.
			continue
		}
		if poll.Error != "" {
			fmt.Println()
			return errors.New(poll.Error)
		}
		if poll.Pending || poll.Token == "" {
			continue
		}

		c := loadCredentials()
		c.Hubs[hub] = hubCredential{
			Token: poll.Token, Login: poll.Login,
			Since: time.Now().Format("2006-01-02"),
		}
		if err := saveCredentials(c); err != nil {
			return err
		}
		path, _ := credentialsPath()
		fmt.Printf("\n\n  signed in as %s\n", poll.Login)
		fmt.Printf("  token stored in %s (readable only by you)\n\n", path)
		return nil
	}
	fmt.Println()
	return errors.New("sign-in timed out — run `still auth login` again")
}

func postJSON(url string, body any, out any) error {
	var buf io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(http.MethodPost, url, buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if len(raw) == 0 {
		return fmt.Errorf("%s returned nothing (%s)", url, resp.Status)
	}
	return json.Unmarshal(raw, out)
}

// openBrowser is a convenience, never a requirement: the URL is printed first,
// so the flow works the same on a headless box where this silently fails.
func openBrowser(url string) {
	if url == "" || strings.HasPrefix(url, "javascript:") {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// ---- published links ----
//
// Sharing works without an account, so for most publishers the revoke token
// issued at publish time is the only thing that can take a link back. It is
// returned exactly once, which makes "write it down yourself" a bad design:
// the CLI keeps it, next to the credentials, readable only by its owner.

type published struct {
	Links map[string]string `json:"links"` // link → revoke token
}

func publishedPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "stillroom", "published.json"), nil
}

func loadPublished() published {
	p := published{Links: map[string]string{}}
	path, err := publishedPath()
	if err != nil {
		return p
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.Links == nil {
		return published{Links: map[string]string{}}
	}
	return p
}

func savePublished(p published) error {
	path, err := publishedPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func rememberPublished(link, token string) error {
	p := loadPublished()
	p.Links[strings.TrimRight(link, "/")] = token
	return savePublished(p)
}

func publishedToken(link string) string { return loadPublished().Links[strings.TrimRight(link, "/")] }

func forgetPublished(link string) {
	p := loadPublished()
	delete(p.Links, strings.TrimRight(link, "/"))
	_ = savePublished(p)
}

// postJSONAuth is postJSON with the hub token attached when there is one, so a
// signed-in publisher can revoke from any machine.
func postJSONAuth(url string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if tok := authToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", url, err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if len(payload) == 0 {
		return fmt.Errorf("%s returned nothing (%s)", url, resp.Status)
	}
	return json.Unmarshal(payload, out)
}
