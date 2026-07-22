// Command still is the Stillroom client: it distills local AI-tool sessions
// into a git-native team knowledge base and materializes that knowledge back
// into agent context. See docs/design-v2.md.
//
// Zero-friction contract (§13): the only human-visible surfaces are
// "install the plugin once" and "review the knowledge diff in the PR".
// Everything here must degrade silently rather than interrupt a session.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xbeekeeper/stillroom/internal/adapter/claudecode"
	"github.com/0xbeekeeper/stillroom/internal/adapter/codex"
	"github.com/0xbeekeeper/stillroom/internal/distill"
	"github.com/0xbeekeeper/stillroom/internal/ir"
	"github.com/0xbeekeeper/stillroom/internal/ledger"
	"github.com/0xbeekeeper/stillroom/internal/materialize"
	"github.com/0xbeekeeper/stillroom/internal/queue"
	"github.com/0xbeekeeper/stillroom/internal/session"
)

// minTurns filters out sessions too short to hold durable knowledge.
const minTurns = 4

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit()
	case "distill":
		err = cmdDistill(os.Args[2:])
	case "materialize":
		err = cmdMaterialize()
	case "status":
		err = cmdStatus()
	case "doctor":
		err = cmdDoctor()
	case "hook":
		err = cmdHook(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "still:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `still — Stillroom: team knowledge distilled from AI coding sessions

Usage:
  still init                        set up .team-context/ in this repo
  still distill                     distill queued + newly discovered sessions
  still distill --transcript PATH   distill one specific transcript
  still distill --dry-run           show proposals without writing files
  still distill --force             include sessions already distilled before
  still materialize                 re-render materialized.md
  still status                      knowledge base, queue and discovery overview
  still doctor                      check the environment end to end
  still hook session-end            (internal) called by the Claude Code plugin
`)
}

// repoStore locates the enclosing git repo and returns its knowledge store.
func repoStore() (ir.Store, error) {
	dir, err := os.Getwd()
	if err != nil {
		return ir.Store{}, err
	}
	for {
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && fi.IsDir() {
			return ir.Store{Root: dir}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ir.Store{}, fmt.Errorf("not inside a git repository")
		}
		dir = parent
	}
}

func mustStore() (ir.Store, error) {
	s, err := repoStore()
	if err != nil {
		return s, err
	}
	if !s.Exists() {
		return s, fmt.Errorf("no %s/ here — run `still init` first", ir.DirName)
	}
	return s, nil
}

func cmdInit() error {
	s, err := repoStore()
	if err != nil {
		return err
	}
	if err := s.Init(); err != nil {
		return err
	}
	added, err := materialize.EnsureImport(filepath.Join(s.Root, "CLAUDE.md"))
	if err != nil {
		return err
	}
	if _, err := materialize.Run(s); err != nil {
		return err
	}
	fmt.Printf("initialized %s/\n", ir.DirName)
	if added {
		fmt.Println("added team-context import to CLAUDE.md")
	}
	fmt.Println("next: `still doctor` to verify the setup, then work normally and `still distill` before your next PR")
	return nil
}

// pendingSessions merges the hook queue with auto-discovered transcripts for
// this repo, skipping what the ledger has already seen (unless force).
func pendingSessions(s ir.Store, force bool) ([]string, error) {
	led := ledger.Open(s.LocalDir())
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		if !force && led.Seen(p) {
			return
		}
		out = append(out, p)
	}

	queued, err := queue.List(s.QueueDir())
	if err != nil {
		return nil, err
	}
	for _, p := range queued {
		add(p)
	}
	discovered, err := claudecode.Discover(claudecode.Home(), s.Root)
	if err != nil {
		return nil, err
	}
	for _, sess := range discovered {
		add(sess.Path)
	}
	// Codex sessions for this repo, discovered the same way. A second tool is
	// just a second adapter — the pipeline downstream is identical.
	codexSessions, err := codex.Discover(codex.Home(), s.Root)
	if err != nil {
		return nil, err
	}
	for _, sess := range codexSessions {
		add(sess.Path)
	}
	return out, nil
}

// digestSession dispatches a transcript path to the adapter that owns its
// format. Queued paths carry no tool tag, so the file shape decides.
func digestSession(path string) (session.Digest, error) {
	if codex.IsRollout(path) {
		return codex.DigestSession(path)
	}
	return claudecode.DigestSession(path)
}

func cmdDistill(args []string) error {
	fs := flag.NewFlagSet("distill", flag.ExitOnError)
	transcript := fs.String("transcript", "", "distill one specific transcript instead of the queue")
	dryRun := fs.Bool("dry-run", false, "print the proposal without writing files")
	force := fs.Bool("force", false, "include sessions the ledger has already seen")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := mustStore()
	if err != nil {
		return err
	}

	var paths []string
	if *transcript != "" {
		paths = []string{*transcript}
	} else {
		paths, err = pendingSessions(s, *force)
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			fmt.Println("nothing to distill — no queued or newly discovered sessions")
			return nil
		}
	}

	facts, _, err := s.LoadFacts()
	if err != nil {
		return err
	}
	existingIDs := make([]string, 0, len(facts))
	for _, f := range facts {
		existingIDs = append(existingIDs, f.ID)
	}
	playbooks, _, err := s.LoadPlaybooks()
	if err != nil {
		return err
	}
	existingPbs := make([]string, 0, len(playbooks))
	for _, p := range playbooks {
		existingPbs = append(existingPbs, p.ID+" — "+p.Title)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	led := ledger.Open(s.LocalDir())

	anyWritten := false
	for _, p := range paths {
		d, err := digestSession(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "still: skip %s: %v\n", p, err)
			continue
		}
		if d.Meta.Turns < minTurns {
			fmt.Printf("skip %s: too short to distill (%d turns)\n", filepath.Base(p), d.Meta.Turns)
			finishSession(s, led, p, d, 0, *dryRun)
			continue
		}
		opts := distill.Options{
			Scope:             "repo:" + filepath.Base(s.Root),
			SourceRef:         sourceRef(d, p),
			ExistingFactIDs:   existingIDs,
			ExistingPlaybooks: existingPbs,
			// Facts are stamped with when the session happened, not when we
			// got around to distilling it — see SessionMeta.LastActivity.
			Now: d.Meta.LastActivity,
		}
		fmt.Printf("distilling %s (%d turns)...\n", filepath.Base(p), d.Meta.Turns)
		prop, err := distill.Run(ctx, distill.ClaudeRunner, d, opts)
		if err != nil {
			return err
		}
		if prop.Redactions > 0 {
			fmt.Printf("  redacted %d secret-shaped strings\n", prop.Redactions)
		}
		for _, f := range prop.Facts {
			if hits := distill.SimilarExisting(f, facts); len(hits) > 0 {
				fmt.Printf("  NOTE %s looks similar to existing %s — consider reusing that id\n",
					f.ID, strings.Join(hits, ", "))
			}
		}
		if *dryRun {
			printProposal(prop)
			continue
		}
		written, err := distill.Apply(s, prop)
		if err != nil {
			return err
		}
		for _, w := range written {
			fmt.Println("  wrote", w)
		}
		if len(written) == 0 {
			fmt.Println("  nothing durable learned in this session")
		} else {
			anyWritten = true
		}
		finishSession(s, led, p, d, len(prop.Facts), false)
	}

	if anyWritten {
		if _, err := materialize.Run(s); err != nil {
			return err
		}
		fmt.Printf("\nreview with: git diff %s/\nthen commit — the knowledge diff rides your normal PR.\n", ir.DirName)
	}
	return nil
}

// finishSession dequeues and (outside dry runs) marks the ledger, so both
// short and distilled sessions stop reappearing.
func finishSession(s ir.Store, led ledger.Ledger, path string, d session.Digest, factCount int, dryRun bool) {
	if dryRun {
		return
	}
	queue.Remove(s.QueueDir(), path)
	_ = led.Mark(ledger.Entry{Transcript: path, SessionID: d.Meta.SessionID, Facts: factCount})
}

func sourceRef(d session.Digest, path string) string {
	if d.Meta.SessionID != "" {
		tool := d.Meta.Tool
		if tool == "" {
			tool = "claude-code"
		}
		return tool + "://" + d.Meta.SessionID
	}
	return "file://" + filepath.Base(path)
}

func printProposal(p distill.Proposal) {
	for _, f := range p.Facts {
		fmt.Printf("\n--- fact: %s [%s] ---\n%s\n", f.ID, f.Confidence, f.Body)
	}
	if p.Playbook != nil {
		fmt.Printf("\n--- playbook: %s (%s) ---\n%s\n", p.Playbook.ID, p.Playbook.Title, p.Playbook.Body)
	}
	if len(p.Facts) == 0 && p.Playbook == nil {
		fmt.Println("(empty proposal)")
	}
}

func cmdMaterialize() error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	summary, err := materialize.Run(s)
	if err != nil {
		return err
	}
	fmt.Println("materialized:", summary)
	return nil
}

func cmdStatus() error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	facts, badFacts, err := s.LoadFacts()
	if err != nil {
		return err
	}
	pbs, badPbs, err := s.LoadPlaybooks()
	if err != nil {
		return err
	}
	pending, err := pendingSessions(s, false)
	if err != nil {
		return err
	}
	active := 0
	for _, f := range facts {
		if f.Status == ir.StatusActive {
			active++
		}
	}
	fmt.Printf("facts: %d (%d active)  playbooks: %d  pending sessions: %d\n",
		len(facts), active, len(pbs), len(pending))
	for name, err := range badFacts {
		fmt.Printf("  BAD fact %s: %v\n", name, err)
	}
	for name, err := range badPbs {
		fmt.Printf("  BAD playbook %s: %v\n", name, err)
	}
	if len(pending) > 0 {
		fmt.Println("run `still distill` to process them")
	}
	return nil
}

func cmdDoctor() error {
	ok := true
	check := func(name string, pass bool, hint string) {
		mark := "ok "
		if !pass {
			mark = "FAIL"
			ok = false
		}
		fmt.Printf("[%s] %s\n", mark, name)
		if !pass && hint != "" {
			fmt.Printf("       → %s\n", hint)
		}
	}

	s, err := repoStore()
	check("inside a git repository", err == nil, "cd into your project repo (or git init)")
	if err != nil {
		return fmt.Errorf("doctor found problems")
	}
	check(ir.DirName+"/ initialized", s.Exists(), "run `still init`")

	claudeMd, _ := os.ReadFile(filepath.Join(s.Root, "CLAUDE.md"))
	check("CLAUDE.md imports team context", strings.Contains(string(claudeMd), materialize.ImportLine),
		"run `still init` (idempotent) to add the import line")

	_, lookErr := exec.LookPath("claude")
	check("claude CLI on PATH (needed by `still distill`)", lookErr == nil,
		"install Claude Code or add it to PATH")

	sessions, _ := claudecode.Discover(claudecode.Home(), s.Root)
	codexSessions, _ := codex.Discover(codex.Home(), s.Root)
	check(fmt.Sprintf("session discovery (%d Claude Code, %d Codex for this repo)",
		len(sessions), len(codexSessions)), true, "")
	if len(sessions)+len(codexSessions) == 0 {
		fmt.Println("       → none found yet: work a session in this repo, or check CLAUDE_CONFIG_DIR / CODEX_HOME")
	}

	if s.Exists() {
		_, badFacts, _ := s.LoadFacts()
		_, badPbs, _ := s.LoadPlaybooks()
		check("knowledge files all parse", len(badFacts) == 0 && len(badPbs) == 0,
			"see `still status` for the broken files")
	}

	if !ok {
		return fmt.Errorf("doctor found problems")
	}
	fmt.Println("all good — work normally, then `still distill` before your next PR")
	return nil
}

// cmdHook handles plugin callbacks. Contract: NEVER block or fail the
// user's session — on any problem, exit 0 silently.
func cmdHook(args []string) error {
	if len(args) < 1 || args[0] != "session-end" {
		return nil // an unknown hook is a plugin/CLI version skew — not the user's problem
	}
	payload, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return nil
	}
	var hook struct {
		TranscriptPath string `json:"transcript_path"`
		CWD            string `json:"cwd"`
	}
	if err := json.Unmarshal(payload, &hook); err != nil || hook.TranscriptPath == "" {
		return nil
	}
	if hook.CWD != "" {
		_ = os.Chdir(hook.CWD)
	}
	s, err := repoStore()
	if err != nil || !s.Exists() {
		return nil // repo hasn't opted in — do nothing
	}
	_ = queue.Enqueue(s.QueueDir(), hook.TranscriptPath)
	return nil
}
