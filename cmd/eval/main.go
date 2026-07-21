// Command eval is the L5 distillation-quality harness (docs/testing.md L5).
//
// It is the one layer that spends real tokens, so it is NOT a Go test and
// never runs in `go test ./...` or CI — you invoke it with `make eval` when
// you have a real `claude` on PATH and want to know whether a BuildPrompt
// change made distillation better or worse.
//
// For each case under testdata/corpus/<name>/ it:
//  1. digests transcript.jsonl through the exact production pipeline,
//  2. distills it with the user's real `claude -p`,
//  3. calls `claude -p` a second time as an LLM-judge, scoring the proposal
//     against expected.md (what a human said the session should teach),
//  4. prints a scorecard and diffs it against eval/baseline.json.
//
// The judge is deliberately a separate model call: distillation quality is
// not something the code can assert, only estimate — recall (did it learn
// what it should), precision (did it invent anything), granularity (are the
// facts the right size).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0xbeekeeper/stillroom/internal/adapter/claudecode"
	"github.com/0xbeekeeper/stillroom/internal/distill"
)

type score struct {
	Recall      int    `json:"recall"`      // 0-5: did it capture what expected.md lists
	Precision   int    `json:"precision"`   // 0-5: freedom from invented/unsupported facts
	Granularity int    `json:"granularity"` // 0-5: facts neither too coarse nor too shattered
	Notes       string `json:"notes"`
}

func (s score) total() int { return s.Recall + s.Precision + s.Granularity }

type result struct {
	Case     string `json:"case"`
	Facts    int    `json:"facts"`
	Playbook bool   `json:"playbook"`
	Score    score  `json:"score"`
	Err      string `json:"err,omitempty"`
}

func main() {
	corpus := flag.String("corpus", "testdata/corpus", "directory of eval cases")
	baseline := flag.String("baseline", "eval/baseline.json", "prior scorecard to diff against")
	out := flag.String("out", "eval/last-run.json", "where to write this run's scorecard")
	list := flag.Bool("list", false, "list cases and exit without spending tokens")
	flag.Parse()

	cases, err := discoverCases(*corpus)
	if err != nil {
		fatal(err)
	}
	if len(cases) == 0 {
		fatal(fmt.Errorf("no cases found under %s (each case is a dir with transcript.jsonl + expected.md)", *corpus))
	}
	if *list {
		for _, c := range cases {
			fmt.Println(c)
		}
		return
	}

	ctx := context.Background()
	results := make([]result, 0, len(cases))
	for _, dir := range cases {
		results = append(results, evalCase(ctx, dir))
	}

	printScorecard(results)
	if err := writeJSON(*out, results); err != nil {
		fatal(err)
	}
	fmt.Printf("\nwrote %s\n", *out)
	diffBaseline(*baseline, results)
}

// discoverCases returns each corpus subdir that has both required files.
func discoverCases(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cases []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if fileExists(filepath.Join(dir, "transcript.jsonl")) && fileExists(filepath.Join(dir, "expected.md")) {
			cases = append(cases, dir)
		}
	}
	sort.Strings(cases)
	return cases, nil
}

func evalCase(ctx context.Context, dir string) result {
	name := filepath.Base(dir)
	r := result{Case: name}

	d, err := claudecode.DigestSession(filepath.Join(dir, "transcript.jsonl"))
	if err != nil {
		r.Err = "digest: " + err.Error()
		return r
	}
	expected, err := os.ReadFile(filepath.Join(dir, "expected.md"))
	if err != nil {
		r.Err = "read expected: " + err.Error()
		return r
	}

	now := d.Meta.LastActivity
	if now.IsZero() {
		now = time.Now()
	}
	prop, err := distill.Run(ctx, distill.ClaudeRunner, d, distill.Options{
		Scope:     "repo:eval",
		SourceRef: "eval://" + name,
		Now:       now,
	})
	if err != nil {
		r.Err = "distill: " + err.Error()
		return r
	}
	r.Facts = len(prop.Facts)
	r.Playbook = prop.Playbook != nil

	sc, err := judge(ctx, string(expected), renderProposal(prop))
	if err != nil {
		r.Err = "judge: " + err.Error()
		return r
	}
	r.Score = sc
	return r
}

// renderProposal flattens a proposal into the plain text the judge sees.
func renderProposal(p distill.Proposal) string {
	var b strings.Builder
	if len(p.Facts) == 0 {
		b.WriteString("(no facts)\n")
	}
	for _, f := range p.Facts {
		fmt.Fprintf(&b, "- fact %s [%s]: %s\n", f.ID, f.Confidence, f.Body)
	}
	if p.Playbook != nil {
		fmt.Fprintf(&b, "\nplaybook %s — %s:\n%s\n", p.Playbook.ID, p.Playbook.Title, p.Playbook.Body)
	}
	return b.String()
}

// judge runs the second model call: score the proposal against the human's
// expectation. It reuses the same ClaudeRunner as distillation.
func judge(ctx context.Context, expected, proposal string) (score, error) {
	prompt := buildJudgePrompt(expected, proposal)
	raw, err := distill.ClaudeRunner(ctx, prompt)
	if err != nil {
		return score{}, err
	}
	return parseScore(raw)
}

func buildJudgePrompt(expected, proposal string) string {
	var b strings.Builder
	b.WriteString(`You are grading an automated knowledge-distillation system. It read a coding
session and produced FACTS (and maybe a playbook). A human wrote, at a topic
level, what the session SHOULD have taught. Grade the produced knowledge
against that expectation on three axes, each 0-5:

- recall: how much of the expected knowledge is present (5 = all of it).
- precision: freedom from invented or unsupported claims (5 = nothing fabricated).
- granularity: are facts the right size — one durable thing each, neither a
  single mushy blob nor shattered into trivia (5 = well-sized).

Respond with ONLY a JSON object, no markdown fence:
{"recall": 0-5, "precision": 0-5, "granularity": 0-5, "notes": "one sentence"}

=== EXPECTED (human, topic-level) ===
`)
	b.WriteString(strings.TrimSpace(expected))
	b.WriteString("\n\n=== PRODUCED (system) ===\n")
	b.WriteString(strings.TrimSpace(proposal))
	b.WriteString("\n")
	return b.String()
}

// parseScore extracts the judge's JSON verdict, tolerating a stray fence.
func parseScore(raw string) (score, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return score{}, fmt.Errorf("no JSON object in judge output: %.120q", raw)
	}
	var s score
	if err := json.Unmarshal([]byte(raw[start:end+1]), &s); err != nil {
		return score{}, fmt.Errorf("bad judge JSON: %w", err)
	}
	return s, nil
}

func printScorecard(results []result) {
	fmt.Printf("\n%-28s %5s %8s  %6s %6s %6s %5s\n", "case", "facts", "playbook", "recall", "prec", "gran", "sum")
	fmt.Println(strings.Repeat("-", 78))
	var sum, n int
	for _, r := range results {
		if r.Err != "" {
			fmt.Printf("%-28s  ERROR: %s\n", r.Case, r.Err)
			continue
		}
		fmt.Printf("%-28s %5d %8v  %6d %6d %6d %5d\n",
			r.Case, r.Facts, r.Playbook, r.Score.Recall, r.Score.Precision, r.Score.Granularity, r.Score.total())
		sum += r.Score.total()
		n++
	}
	if n > 0 {
		fmt.Println(strings.Repeat("-", 78))
		fmt.Printf("%-28s %5s %8s  %6s %6s %6s %5.1f (mean of %d)\n", "TOTAL", "", "", "", "", "", float64(sum)/float64(n), n)
	}
}

// diffBaseline prints the per-case sum delta against a prior run, so a prompt
// change's effect on quality is visible instead of vibes.
func diffBaseline(path string, results []result) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("(no baseline at %s — commit this run as one with: cp %s %s)\n", path, "eval/last-run.json", path)
		return
	}
	var base []result
	if err := json.Unmarshal(data, &base); err != nil {
		fmt.Printf("(baseline %s is unreadable: %v)\n", path, err)
		return
	}
	prior := map[string]int{}
	for _, r := range base {
		prior[r.Case] = r.Score.total()
	}
	fmt.Printf("\nvs baseline %s:\n", path)
	for _, r := range results {
		if r.Err != "" {
			continue
		}
		was, ok := prior[r.Case]
		if !ok {
			fmt.Printf("  %-28s NEW  %d\n", r.Case, r.Score.total())
			continue
		}
		delta := r.Score.total() - was
		fmt.Printf("  %-28s %+d  (%d → %d)\n", r.Case, delta, was, r.Score.total())
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "eval:", err)
	os.Exit(1)
}
