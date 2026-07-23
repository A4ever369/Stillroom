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

const defaultHub = "https://still.sh"

func hubBase() string {
	if v := strings.TrimRight(os.Getenv("STILLROOM_HUB"), "/"); v != "" {
		return v
	}
	return defaultHub
}

func cmdPublish(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	note := fs.String("note", "", "one line describing what this is (\"how our deploy actually works\")")
	full := fs.Bool("full", false, "include redacted session transcripts, not just the distilled knowledge")
	out := fs.String("out", "", "write the pack to a file instead of uploading")
	yes := fs.Bool("yes", false, "skip the confirmation prompt (for scripts; think before using it)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	s, err := mustStore()
	if err != nil {
		return err
	}

	mode := pack.ModeKnowledge
	if *full {
		mode = pack.ModeFull
	}

	// In full mode the evidence is the sessions this knowledge came from. Only
	// sessions belonging to this repo are eligible — never the whole machine.
	var digests []session.Digest
	if mode == pack.ModeFull {
		paths, err := pendingSessions(s, true)
		if err != nil {
			return err
		}
		sortByMtimeDesc(paths)
		if len(paths) > 5 {
			paths = paths[:5]
		}
		for _, p := range paths {
			d, err := digestSession(p)
			if err != nil {
				continue
			}
			digests = append(digests, d)
		}
	}

	p, err := pack.Build(s, digests, mode, *note, originOf(s))
	if err != nil {
		return err
	}
	if len(p.Facts) == 0 && len(p.Playbooks) == 0 {
		return errors.New("nothing to share yet — run `still distill` first")
	}

	printPackSummary(p)

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

	link, token, err := uploadPack(p)
	if err != nil {
		return err
	}
	// Remember the revoke token here rather than making the publisher copy it
	// out of terminal scrollback. It is the only way most people can take a
	// link back, and it is issued exactly once.
	if token != "" {
		if err := rememberPublished(link, token); err != nil {
			fmt.Printf("\n  (could not save the revoke token: %v)\n", err)
			fmt.Printf("  keep it somewhere safe: %s\n", token)
		}
	}

	fmt.Printf("\n  %s\n\n", link)
	fmt.Println("Send that to anyone. They paste this into their own Claude Code or Codex:")
	fmt.Printf("\n  still pull %s\n\n", link)
	fmt.Printf("Changed your mind later:  still revoke %s\n", link)
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
