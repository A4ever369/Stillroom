package main

// Machine-enforced versions of the two hard rules that were previously kept
// only by discipline: zero dependencies, and "repeated runs may not produce
// a git diff" (CLAUDE.md, docs/testing.md L0/L2).

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Zero dependencies is a load-bearing promise, not a preference: `still` is
// installed into other people's toolchains, and the tiny frontmatter parser
// in internal/ir exists precisely so no YAML library is needed.
func TestZeroDependencies(t *testing.T) {
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "require") || strings.HasPrefix(line, "replace") {
			t.Errorf("go.mod gained a dependency — stdlib only is a hard rule:\n\t%s", line)
		}
	}
	if _, err := os.Stat("../../go.sum"); err == nil {
		t.Error("go.sum exists — something pulled in a dependency")
	}
}

// The determinism rule stated in the terms that actually matter to a user:
// after committing, re-running the tool must leave the working tree clean.
func TestRepeatedRunsProduceNoGitDiff(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	w.fakeClaude(proposal(okProposal))
	w.session("sess.jsonl", longSession()...)

	if got := w.run("distill"); got.code != 0 {
		t.Fatalf("distill failed: %s", got.out())
	}
	w.gitCommit("knowledge")

	// Re-running the read-only commands must not dirty anything.
	for _, args := range [][]string{{"materialize"}, {"materialize"}, {"status"}, {"doctor"}, {"distill"}} {
		w.run(args...)
		if dirty := w.gitStatus(); dirty != "" {
			t.Fatalf("`still %s` dirtied the tree — repeated runs must be byte-stable:\n%s",
				strings.Join(args, " "), dirty)
		}
	}

	// Even re-distilling the same session must land on identical bytes.
	if got := w.run("distill", "--force"); got.code != 0 {
		t.Fatalf("forced distill failed: %s", got.out())
	}
	if dirty := w.gitStatus(); dirty != "" {
		t.Errorf("re-distilling the same session changed the knowledge base:\n%s", dirty)
	}
}

func (w *world) git(args ...string) string {
	w.t.Helper()
	full := append([]string{"-C", w.repo,
		"-c", "user.email=test@stillroom.invalid",
		"-c", "user.name=stillroom test",
	}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (w *world) gitCommit(msg string) {
	w.t.Helper()
	w.git("add", "-A")
	w.git("commit", "-q", "-m", msg)
}

// gitStatus returns the porcelain status, empty when the tree is clean.
func (w *world) gitStatus() string {
	w.t.Helper()
	return strings.TrimSpace(w.git("status", "--porcelain"))
}
