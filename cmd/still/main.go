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
	"sort"
	"strings"
	"time"

	"github.com/A4ever369/Stillroom/internal/adapter/claudecode"
	"github.com/A4ever369/Stillroom/internal/adapter/codex"
	"github.com/A4ever369/Stillroom/internal/distill"
	"github.com/A4ever369/Stillroom/internal/ir"
	"github.com/A4ever369/Stillroom/internal/ledger"
	"github.com/A4ever369/Stillroom/internal/materialize"
	"github.com/A4ever369/Stillroom/internal/queue"
	"github.com/A4ever369/Stillroom/internal/review"
	"github.com/A4ever369/Stillroom/internal/session"
)

// minTurns filters out sessions too short to hold durable knowledge.
const minTurns = 4

// version is stamped at build time by GoReleaser (-X main.version=…); it stays
// "dev" for plain `go build`/`go install` of an untagged tree.
var version = "dev"

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
		err = cmdMaterialize(os.Args[2:])
	case "review":
		err = cmdReview(os.Args[2:])
	case "publish":
		err = cmdPublish(os.Args[2:])
	case "pull":
		err = cmdPull(os.Args[2:])
	case "auth":
		err = cmdAuth(os.Args[2:])
	case "revoke":
		err = cmdRevoke(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "doctor":
		err = cmdDoctor()
	case "hook":
		err = cmdHook(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("still", version)
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
  still distill --transcript PATH   distill one transcript file, or a whole folder of .jsonl
  still distill --dry-run           show proposals without writing files
  still distill --force             include sessions already distilled before
  still distill --limit N           distill at most N sessions, newest first
  still materialize                 re-render materialized.md
  still materialize --check         verify materialized.md is current (exit 1 if stale)
  still review --base DIR            print a knowledge diff vs another checkout (for PR bots)
  still publish                     share this knowledge as a link (--full to include sessions)
  still pull <link|file>            receive someone else's knowledge pack
  still revoke <link>               take a published link back
  still auth login | status | logout  sign in so your packs carry your name
  still status [--json]             knowledge base, queue and discovery overview
  still doctor                      check the environment end to end
  still hook session-end            (internal) called by the Claude Code plugin
  still version                     print the build version
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

// collectTranscripts walks dir (recursively) for *.jsonl transcript files, so
// `still distill --transcript <folder>` distills a whole batch in one command.
func collectTranscripts(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// sortByMtimeDesc orders transcript paths newest first by file mtime. An
// unreadable path sorts last rather than aborting the run.
func sortByMtimeDesc(paths []string) {
	mtime := func(p string) int64 {
		if info, err := os.Stat(p); err == nil {
			return info.ModTime().UnixNano()
		}
		return 0
	}
	sort.Slice(paths, func(i, j int) bool { return mtime(paths[i]) > mtime(paths[j]) })
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
	transcript := fs.String("transcript", "", "distill a specific transcript file, or every .jsonl under a directory")
	dryRun := fs.Bool("dry-run", false, "print the proposal without writing files")
	force := fs.Bool("force", false, "include sessions the ledger has already seen")
	limit := fs.Int("limit", 0, "distill at most N sessions this run, newest first (0 = all)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := mustStore()
	if err != nil {
		return err
	}

	var paths []string
	singleFile := false
	switch {
	case *transcript != "":
		// A directory distills the whole folder in one command; a file (or a
		// path that does not exist) stays a single entry, so a missing file is
		// skipped non-fatally by the loop below rather than aborting the run.
		if info, serr := os.Stat(*transcript); serr == nil && info.IsDir() {
			paths, err = collectTranscripts(*transcript)
			if err != nil {
				return err
			}
			if len(paths) == 0 {
				fmt.Printf("no .jsonl transcripts under %s\n", *transcript)
				return nil
			}
		} else {
			paths = []string{*transcript}
			singleFile = true
		}
	default:
		paths, err = pendingSessions(s, *force)
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			fmt.Println("nothing to distill — no queued or newly discovered sessions")
			return nil
		}
	}

	// For any batch (discovery or a folder), order newest-first so --limit keeps
	// the most recent work, and make the per-session model-call cost visible.
	if !singleFile {
		sortByMtimeDesc(paths)
		total := len(paths)
		if *limit > 0 && *limit < total {
			fmt.Printf("%d sessions pending; processing the %d most recent (--limit).\n", total, *limit)
			paths = paths[:*limit]
		} else {
			fmt.Printf("%d session(s) to distill — each is a `claude -p` model call.\n", total)
			if total > 5 {
				fmt.Println("tip: use --limit N to cap this run to the N most recent.")
			}
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

func cmdMaterialize(args []string) error {
	fs := flag.NewFlagSet("materialize", flag.ExitOnError)
	check := fs.Bool("check", false, "verify materialized.md is up to date without writing (exit 1 if stale)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := mustStore()
	if err != nil {
		return err
	}
	if *check {
		want, _, err := materialize.Render(s)
		if err != nil {
			return err
		}
		got, _ := os.ReadFile(s.MaterializedPath())
		if string(got) != want {
			return fmt.Errorf("materialized.md is stale — run `still materialize` and commit the result")
		}
		fmt.Println("materialized.md is up to date")
		return nil
	}
	summary, err := materialize.Run(s)
	if err != nil {
		return err
	}
	fmt.Println("materialized:", summary)
	return nil
}

// cmdReview prints a human-readable knowledge diff (the §13 "review parasite"
// summary) to stdout. It compares two knowledge-base roots: --head (default:
// this repo) against --base (default: empty, i.e. everything is new). A CI
// workflow points --base at a checkout of the target branch and posts the
// output as a PR comment. It never fails a build: a bad snapshot is reported
// but still exits 0.
func cmdReview(args []string) error {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	baseDir := fs.String("base", "", "knowledge-base root to compare against (default: empty)")
	headDir := fs.String("head", "", "knowledge-base root under review (default: this repo)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var head review.Snapshot
	if *headDir != "" {
		head = loadSnapshot(ir.Store{Root: *headDir})
	} else {
		s, err := mustStore()
		if err != nil {
			return err
		}
		head = loadSnapshot(s)
	}

	var base review.Snapshot
	if *baseDir != "" {
		base = loadSnapshot(ir.Store{Root: *baseDir})
	}

	fmt.Print(review.Diff(base, head).Markdown())
	return nil
}

// loadSnapshot reads a store's active knowledge. Unparseable files are skipped
// (their warnings surface in materialize/status, not here) so a review comment
// is never blocked by one bad merge.
func loadSnapshot(s ir.Store) review.Snapshot {
	facts, _, _ := s.LoadFacts()
	pbs, _, _ := s.LoadPlaybooks()
	return review.Snapshot{Facts: facts, Playbooks: pbs}
}

// statusReport is the machine-readable knowledge-base overview. The text and
// --json renderings are both derived from it, so they can never disagree.
type statusReport struct {
	Facts struct {
		Total  int `json:"total"`
		Active int `json:"active"`
		Bad    int `json:"bad"`
	} `json:"facts"`
	Playbooks struct {
		Total int `json:"total"`
		Bad   int `json:"bad"`
	} `json:"playbooks"`
	PendingSessions int `json:"pending_sessions"`
	Discovery       struct {
		ClaudeCode int `json:"claude_code"`
		Codex      int `json:"codex"`
	} `json:"discovery"`
	BadFiles             []string `json:"bad_files"`
	MaterializedUpToDate bool     `json:"materialized_up_to_date"`
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emit a machine-readable JSON report")
	if err := fs.Parse(args); err != nil {
		return err
	}
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

	var rep statusReport
	rep.Facts.Total = len(facts)
	for _, f := range facts {
		if f.Status == ir.StatusActive {
			rep.Facts.Active++
		}
	}
	rep.Facts.Bad = len(badFacts)
	rep.Playbooks.Total = len(pbs)
	rep.Playbooks.Bad = len(badPbs)
	rep.PendingSessions = len(pending)
	if cc, derr := claudecode.Discover(claudecode.Home(), s.Root); derr == nil {
		rep.Discovery.ClaudeCode = len(cc)
	}
	if cx, derr := codex.Discover(codex.Home(), s.Root); derr == nil {
		rep.Discovery.Codex = len(cx)
	}
	rep.BadFiles = []string{} // never null in JSON
	for name := range badFacts {
		rep.BadFiles = append(rep.BadFiles, ir.DirName+"/facts/"+name)
	}
	for name := range badPbs {
		rep.BadFiles = append(rep.BadFiles, ir.DirName+"/playbooks/"+name)
	}
	sort.Strings(rep.BadFiles)
	if want, _, rerr := materialize.Render(s); rerr == nil {
		got, _ := os.ReadFile(s.MaterializedPath())
		rep.MaterializedUpToDate = string(got) == want
	}

	if *asJSON {
		out, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("facts: %d (%d active)  playbooks: %d  pending sessions: %d\n",
		rep.Facts.Total, rep.Facts.Active, rep.Playbooks.Total, rep.PendingSessions)
	for _, bad := range rep.BadFiles {
		fmt.Printf("  BAD %s\n", bad)
	}
	if !rep.MaterializedUpToDate {
		fmt.Println("materialized.md is stale — run `still materialize` and commit")
	}
	if rep.PendingSessions > 0 {
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

		// materialized.md is committed alongside facts/; if a hand edit or a
		// merge changed facts without a re-render, teammates load stale context.
		if want, _, rerr := materialize.Render(s); rerr == nil {
			got, _ := os.ReadFile(s.MaterializedPath())
			check("materialized.md is up to date", string(got) == want,
				"run `still materialize` and commit the result")
		}
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
