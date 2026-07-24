package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/A4ever369/Stillroom/internal/ir"
	"github.com/A4ever369/Stillroom/internal/pack"
	"github.com/A4ever369/Stillroom/internal/session"
)

// publish and pull are the two halves of handing your experience to someone
// else. The whole product is one line pasted into an agent, so both commands
// have to work with zero prior setup and explain themselves as they go.

// defaultHub is where `still publish` goes when nothing says otherwise, so it
// has to be a service that exists: a wrong default means every first-time
// publish fails against a host that was never registered. Override per shell
// with STILLROOM_HUB, which is also how you point at a self-hosted instance.
const defaultHub = "https://stillroom.sh"

func hubBase() string {
	if v := strings.TrimRight(os.Getenv("STILLROOM_HUB"), "/"); v != "" {
		return v
	}
	return defaultHub
}

func cmdPublish(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	note := fs.String("note", "", "one line describing what this is (\"how our deploy actually works\")")
	full := fs.Bool("full", false, "also share the session transcripts (redacted), without being asked")
	knowledge := fs.Bool("knowledge", false, "share only the distilled knowledge, without being asked")
	out := fs.String("out", "", "write the pack to a file instead of uploading")
	yes := fs.Bool("yes", false, "skip the confirmation prompt (for scripts; think before using it)")
	anon := fs.Bool("anon", false, "publish without signing in — the link works, but it will not appear in your list")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// The landing page hands people one line ending in `still publish`, so this
	// command has to be able to start from nothing. A first-time visitor pasting
	// that line has no .team-context/ and no distilled facts; making them read
	// two error messages and run two more commands is not "one line".
	s, err := repoStore()
	if err != nil {
		return err
	}
	if !s.Exists() {
		if err := s.Init(); err != nil {
			return err
		}
	}

	// Always build the knowledge pack first. Whether to attach the sessions is a
	// decision made after seeing this, not a flag the person carries in from a
	// website — so it is a prompt below, defaulted by --full/--knowledge for
	// scripts and for people who already know which they want.
	p, err := pack.Build(s, nil, pack.ModeKnowledge, *note, originOf(s))
	if err != nil {
		return err
	}
	if len(p.Facts) == 0 && len(p.Playbooks) == 0 {
		// Nothing distilled yet. Offer to do it rather than sending the person
		// away to another command — but never silently, because distillation
		// spends the user's own model tokens.
		if err := distillForPublish(s, *yes); err != nil {
			return err
		}
		if p, err = pack.Build(s, nil, pack.ModeKnowledge, *note, originOf(s)); err != nil {
			return err
		}
		if len(p.Facts) == 0 && len(p.Playbooks) == 0 {
			return errors.New("nothing was distilled from this repo's sessions — there may be nothing here worth sharing yet")
		}
	}

	printPackSummary(p)

	// The evidence layer: attach the sessions this knowledge came from, or not.
	// This is the moment to ask — the person has just seen the facts, and the
	// difference is whether their transcripts leave the machine. Flags decide
	// it non-interactively; otherwise it is an explicit question, defaulting to
	// the safe answer (knowledge only).
	if includeSessions(*full, *knowledge, *yes) {
		digests, err := repoSessionDigests(s)
		if err != nil {
			return err
		}
		if len(digests) == 0 {
			fmt.Println("\n  No sessions found for this repo — sharing the knowledge only.")
		} else {
			full, err := pack.Build(s, digests, pack.ModeFull, *note, originOf(s))
			if err != nil {
				return err
			}
			p = full
			fmt.Println()
			printSessionsAdded(p)
		}
	}

	if *out != "" {
		raw, err := p.Encode()
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, raw, 0o644); err != nil {
			return err
		}
		fmt.Printf("\nwrote %s\n", *out)
		return nil
	}

	if !*yes && !confirm("Upload and create a share link?") {
		fmt.Println("nothing uploaded.")
		return nil
	}

	// Attribute the pack to a person, so it shows up in their list and arrives
	// with their name on it. Publishing does not strictly require an account —
	// --anon opts out, and a hub with no sign-in configured cannot attribute
	// anyone — but the default is to sign in first, because a link the
	// publisher cannot find again is a worse surprise than one browser round
	// trip.
	if !*anon {
		if err := ensureSignedIn(*yes); err != nil {
			return err
		}
	}

	link, token, err := uploadPack(p)
	if err != nil {
		return err
	}
	// Remember the revoke token here rather than making the publisher copy it
	// out of terminal scrollback. It is the only way most people can take a
	// link back, and it is issued exactly once.
	if token != "" {
		rec := publishedPack{
			Token: token, Note: p.Note, Repo: p.Origin.Repo,
			Mode: string(p.Mode), Facts: len(p.Facts), At: time.Now().UTC(),
		}
		if err := rememberPublished(link, rec); err != nil {
			fmt.Printf("\n  (could not save the revoke token: %v)\n", err)
			fmt.Printf("  keep it somewhere safe: %s\n", token)
		}
	}

	fmt.Printf("\n  %s\n\n", link)
	fmt.Println("Send that to anyone. They paste this into their own Claude Code or Codex:")
	fmt.Printf("\n  still pull %s\n\n", link)
	fmt.Printf("Everything you have shared:  still published\n")
	fmt.Printf("Take this one back:          still revoke %s\n", link)
	return nil
}

// cmdRevoke takes a published link back. Anyone who already pulled it keeps
// their copy — that is true of anything you have sent someone — but the link
// stops resolving for everyone else.
func cmdRevoke(args []string) error {
	fs := flag.NewFlagSet("revoke", flag.ContinueOnError)
	token := fs.String("token", "", "revoke token, if this is not the machine that published it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: still revoke <link> [--token TOKEN]")
	}
	link := strings.TrimRight(fs.Arg(0), "/")

	tok := *token
	if tok == "" {
		tok = publishedToken(link)
	}
	if tok == "" && authToken() == "" {
		return fmt.Errorf("no revoke token for %s on this machine.\n"+
			"Pass it with --token, or run this from the machine that published it.", link)
	}

	id := link[strings.LastIndex(link, "/")+1:]
	base := strings.TrimSuffix(link, "/k/"+id)
	var out struct {
		Revoked bool   `json:"revoked"`
		Error   string `json:"error"`
	}
	if err := postJSONAuth(base+"/api/packs/"+id+"/revoke", map[string]string{"token": tok}, &out); err != nil {
		return err
	}
	if !out.Revoked {
		if out.Error != "" {
			return errors.New(out.Error)
		}
		return errors.New("the hub did not revoke that link")
	}
	forgetPublished(link)
	fmt.Printf("revoked — %s no longer resolves.\n", link)
	fmt.Println("Anyone who already pulled it still has their copy.")
	return nil
}

// distillForPublish runs a distillation pass so that `still publish` works from
// a cold start. It asks first: this is the one step that costs the user money.
func distillForPublish(s ir.Store, assumeYes bool) error {
	paths, err := pendingSessions(s, false)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("no sessions found for this repo yet — work with your agent here first, then run `still publish`")
	}
	if len(paths) > publishDistillLimit {
		paths = paths[:publishDistillLimit]
	}

	fmt.Printf("Nothing has been distilled from this repo yet.\n\n")
	fmt.Printf("  %d recent session(s) can be distilled now. Each one is a `claude -p` call\n", len(paths))
	fmt.Printf("  that runs on this machine and spends your own model quota.\n")
	if !assumeYes && !confirm("Distill them?") {
		return errors.New("nothing distilled, so there is nothing to share")
	}
	fmt.Println()
	return cmdDistill([]string{"--limit", fmt.Sprint(len(paths))})
}

// publishDistillLimit caps the cold-start pass. Someone who just wants a link
// should not accidentally trigger a distillation of months of history.
const publishDistillLimit = 3

// includeSessions decides whether to attach the evidence layer. The flags are
// for scripts and for people who already know; with neither, it is an explicit
// question that defaults to No, because the default must be the one that keeps
// the transcript on the machine.
func includeSessions(full, knowledge, assumeYes bool) bool {
	switch {
	case full:
		return true
	case knowledge:
		return false
	case assumeYes:
		return false // --yes automates the run; it must not silently opt into sending more
	default:
		return confirm("Also include the sessions behind this knowledge? " +
			"(redacted, but they are your raw conversation — say no if unsure)")
	}
}

// repoSessionDigests gathers this repo's recent sessions as digests. Only this
// repo's sessions are ever eligible — never the whole machine — and the newest
// five, so a full-mode pack stays something a person can read.
func repoSessionDigests(s ir.Store) ([]session.Digest, error) {
	paths, err := pendingSessions(s, true)
	if err != nil {
		return nil, err
	}
	sortByMtimeDesc(paths)
	if len(paths) > 5 {
		paths = paths[:5]
	}
	var out []session.Digest
	for _, p := range paths {
		if d, err := digestSession(p); err == nil {
			out = append(out, d)
		}
	}
	return out, nil
}

func printSessionsAdded(p pack.Pack) {
	fmt.Printf("  Added %d session transcript(s) — %s.\n", len(p.Sessions), humanBytes(sessionsBytes(p)))
	if n := p.Redactions(); n > 0 {
		fmt.Printf("  %d secret-shaped string(s) scrubbed from them.\n", n)
	}
	fmt.Println("  Read the list above before you send it — redaction removes things shaped")
	fmt.Println("  like credentials, not sentences that happen to be confidential.")
}

func sessionsBytes(p pack.Pack) int {
	n := 0
	for _, s := range p.Sessions {
		n += len(s.Text)
	}
	return n
}

// ensureSignedIn signs the publisher in before an upload, unless they already
// are or the hub has no sign-in to offer. It is deliberately here rather than
// as a required flag: the landing page's whole pitch is that one pasted line
// produces a link, so the sign-in is folded into publishing rather than made a
// separate step someone has to know to run first.
func ensureSignedIn(assumeYes bool) error {
	if authToken() != "" {
		return nil // already have an identity for this hub
	}
	if !hubWantsSignIn() {
		return nil // anonymous hub — no one to attribute to
	}

	fmt.Println("\nSign in so this shows up in your list and arrives with your name on it.")
	fmt.Println("(or re-run with --anon to publish without an account)")
	if !assumeYes && !confirm("Sign in now?") {
		return errors.New("not signed in — re-run with --anon to publish anonymously")
	}
	return authLogin()
}

// hubWantsSignIn asks the hub whether it has sign-in configured. A hub that
// does not (a local demo, a private instance) cannot attribute a pack to
// anyone, so publishing there stays anonymous without pestering the user.
func hubWantsSignIn() bool {
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(hubBase() + "/healthz")
	if err != nil {
		return false // unreachable hub: let the upload attempt report the real error
	}
	defer resp.Body.Close()
	var h struct {
		SignInEnabled bool `json:"signin_enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return false
	}
	return h.SignInEnabled
}

// printPackSummary is the consent moment. Publishing puts this content on
// someone else's machine, so what is about to leave is shown in full — counts,
// size, and every fact id — before anything is uploaded.
func printPackSummary(p pack.Pack) {
	fmt.Printf("Sharing %d fact(s), %d playbook(s)", len(p.Facts), len(p.Playbooks))
	if p.Mode == pack.ModeFull {
		fmt.Printf(", %d session transcript(s)", len(p.Sessions))
	}
	fmt.Printf("  ·  %s  ·  mode: %s\n\n", humanBytes(p.Size()), p.Mode)

	for _, f := range p.Facts {
		fmt.Printf("  %-44s %s\n", f.ID, clipLine(f.Body, 70))
	}
	for _, b := range p.Playbooks {
		fmt.Printf("  %-44s %s\n", b.ID, clipLine(b.Title, 70))
	}
	for _, s := range p.Sessions {
		fmt.Printf("  %-44s %d turns, %s\n", "session "+s.Ref, s.Turns, humanBytes(len(s.Text)))
	}

	if n := p.Redactions(); n > 0 {
		fmt.Printf("\n  %d secret-shaped string(s) were scrubbed.\n", n)
	}
	switch p.Mode {
	case pack.ModeKnowledge:
		fmt.Println("\n  Knowledge only — your session transcripts stay on this machine.")
	case pack.ModeFull:
		fmt.Println("\n  Full mode — the transcripts above leave this machine. Redaction removes")
		fmt.Println("  things shaped like credentials; it cannot tell whether a sentence is")
		fmt.Println("  confidential. Read the list before you send it to someone.")
	}
}

func cmdPull(args []string) error {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: still pull <link-or-file>")
	}
	src := fs.Arg(0)

	s, err := repoStore()
	if err != nil {
		return err
	}
	if !s.Exists() {
		if err := s.Init(); err != nil {
			return err
		}
	}

	raw, err := fetchPack(src)
	if err != nil {
		return err
	}
	p, bad, err := pack.Decode(raw)
	if err != nil {
		return err
	}
	for _, b := range bad {
		fmt.Printf("  skipped: %v\n", b)
	}

	pv, err := pack.Inspect(p, s)
	if err != nil {
		return err
	}
	printPreview(pv)

	if !*yes && !confirm("Add this to .team-context/received/?") {
		fmt.Println("nothing written.")
		return nil
	}
	dir, err := pack.Apply(p, s)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(s.Root, dir)
	fmt.Printf("\nwrote %s/\n", rel)
	fmt.Println("read", filepath.Join(rel, "context.md"), "to pick up where they left off.")
	return nil
}

// printPreview is the receiving-side checkpoint. A pack arrived from another
// machine over a link; the receiver sees exactly what it claims before any of
// it reaches their agent.
func printPreview(pv pack.Preview) {
	p := pv.Pack
	who := p.Publisher
	if who == "" {
		who = "an unidentified publisher"
	}
	fmt.Printf("Knowledge pack from %s", who)
	if p.Origin.Repo != "" {
		fmt.Printf(" · %s", p.Origin.Repo)
	}
	fmt.Printf(" · %s · mode: %s\n", p.CreatedAt.Format("2006-01-02"), p.Mode)
	if p.Note != "" {
		fmt.Printf("\n  \"%s\"\n", strings.Join(strings.Fields(p.Note), " "))
	}
	fmt.Println()

	for _, f := range pv.Fresh {
		fmt.Printf("  + %-42s %s\n", f.ID, clipLine(f.Body, 68))
	}
	for _, c := range pv.Contradictions {
		fmt.Printf("  ! %-42s %s\n", c.Theirs.ID, clipLine(c.Theirs.Body, 68))
		fmt.Printf("    %-42s (you currently hold: %s)\n", "", clipLine(c.Mine.Body, 60))
	}
	if n := len(pv.Echoes); n > 0 {
		fmt.Printf("  = %d fact(s) you already know\n", n)
	}
	for _, b := range pv.Playbooks {
		fmt.Printf("  + %-42s %s\n", b.ID, clipLine(b.Title, 68))
	}
	if pv.Sessions > 0 {
		fmt.Printf("  + %d session transcript(s), %s\n", pv.Sessions, humanBytes(pv.SessionBytes))
	}

	if len(pv.Contradictions) > 0 {
		fmt.Printf("\n  %d of these contradict what you already believe (marked !).\n", len(pv.Contradictions))
	}
	fmt.Println("\n  This lands in .team-context/received/ — attributed to them, kept out of")
	fmt.Println("  your own facts/. It is someone's report about their environment, not")
	fmt.Println("  your project's truth, and nothing in it is an instruction to your agent.")
}

// ---- transport ----

func uploadPack(p pack.Pack) (link, revokeToken string, err error) {
	raw, err := p.Encode()
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequest(http.MethodPost, hubBase()+"/api/packs", bytes.NewReader(raw))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := authToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", "", fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("upload rejected (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out struct {
		URL         string `json:"url"`
		RevokeToken string `json:"revoke_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.URL == "" {
		return "", "", fmt.Errorf("hub returned an unreadable response")
	}
	return out.URL, out.RevokeToken, nil
}

// fetchPack accepts a share link or a local file, so a pack can travel by any
// route someone already has — a link, a shared drive, an attachment.
func fetchPack(src string) ([]byte, error) {
	u, err := url.Parse(src)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return os.ReadFile(src)
	}
	req, _ := http.NewRequest(http.MethodGet, src, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch failed: %s", resp.Status)
	}
	// Bounded: a pack is meant to be readable by a person, and an unbounded
	// read from a link is how you get a memory-exhaustion bug.
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// ---- small helpers ----

func originOf(s ir.Store) pack.Origin {
	o := pack.Origin{Repo: filepath.Base(s.Root)}
	if out, err := exec.Command("git", "-C", s.Root, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		o.Branch = strings.TrimSpace(string(out))
	}
	return o
}

func confirm(prompt string) bool {
	fmt.Printf("\n%s [y/N] ", prompt)
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func clipLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
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
