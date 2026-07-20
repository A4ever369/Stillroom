package main

// Black-box coverage of every CLI command, including the failure paths a real
// `claude` can never be made to produce on demand (malformed proposals,
// non-zero exits). See docs/testing.md L3.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	t.Run("fresh repo", func(t *testing.T) {
		w := newWorld(t)
		got := w.run("init")
		if got.code != 0 {
			t.Fatalf("exit=%d: %s", got.code, got.out())
		}
		for _, want := range []string{".team-context", "CLAUDE.md", ".team-context/materialized.md"} {
			if !w.exists(want) {
				t.Errorf("init did not create %s", want)
			}
		}
		if !strings.Contains(w.read("CLAUDE.md"), ".team-context/materialized.md") {
			t.Error("CLAUDE.md missing the team-context import")
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		first := w.read("CLAUDE.md")
		if got := w.run("init"); got.code != 0 {
			t.Fatalf("second init failed: %s", got.out())
		}
		if second := w.read("CLAUDE.md"); second != first {
			t.Errorf("second init changed CLAUDE.md:\n--- first ---\n%s\n--- second ---\n%s", first, second)
		}
	})

	t.Run("preserves an existing CLAUDE.md", func(t *testing.T) {
		w := newWorld(t)
		existing := "# My project\n\nSome rules I wrote by hand.\n"
		if err := os.WriteFile(filepath.Join(w.repo, "CLAUDE.md"), []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
		w.run("init")
		got := w.read("CLAUDE.md")
		if !strings.Contains(got, "Some rules I wrote by hand.") {
			t.Errorf("init clobbered the user's CLAUDE.md:\n%s", got)
		}
		if !strings.Contains(got, ".team-context/materialized.md") {
			t.Errorf("init did not add the import:\n%s", got)
		}
	})

	t.Run("outside a git repo", func(t *testing.T) {
		w := newWorld(t)
		outside := filepath.Dir(w.repo)
		got := w.runIn(outside, "", "init")
		if got.code == 0 {
			t.Errorf("expected failure outside a git repo, got:\n%s", got.out())
		}
		if !strings.Contains(got.stderr, "git repositor") {
			t.Errorf("error should name the cause, got: %s", got.stderr)
		}
	})
}

func TestCommandsRequireInit(t *testing.T) {
	for _, cmd := range []string{"distill", "materialize", "status"} {
		t.Run(cmd, func(t *testing.T) {
			w := newWorld(t)
			got := w.run(cmd)
			if got.code == 0 {
				t.Errorf("%s should fail before init, got:\n%s", cmd, got.out())
			}
			if !strings.Contains(got.stderr, "still init") {
				t.Errorf("error should point at `still init`, got: %s", got.stderr)
			}
		})
	}
}

func TestDistill(t *testing.T) {
	t.Run("no sessions", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		w.fakeClaude(proposal(okProposal))
		got := w.run("distill")
		if got.code != 0 {
			t.Fatalf("exit=%d: %s", got.code, got.out())
		}
		if !strings.Contains(got.stdout, "nothing to distill") {
			t.Errorf("want a clear no-op message, got:\n%s", got.out())
		}
	})

	t.Run("session below minTurns is skipped", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		w.fakeClaude(proposal(okProposal))
		w.session("short.jsonl",
			`{"type":"user","sessionId":"s","message":{"content":"hi"}}`,
			`{"type":"assistant","message":{"content":"hello"}}`)

		got := w.run("distill")
		if got.code != 0 {
			t.Fatalf("exit=%d: %s", got.code, got.out())
		}
		if !strings.Contains(got.stdout, "too short") {
			t.Errorf("want a skip message, got:\n%s", got.out())
		}
		if w.exists(".team-context/facts/deploy.acme.db-endpoint.md") {
			t.Error("a too-short session must not produce facts")
		}
	})

	t.Run("writes facts and playbook", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		w.fakeClaude(proposal(okProposal))
		w.session("sess.jsonl", longSession()...)

		got := w.run("distill")
		if got.code != 0 {
			t.Fatalf("exit=%d: %s", got.code, got.out())
		}
		if !w.exists(".team-context/facts/deploy.acme.db-endpoint.md") {
			t.Fatalf("fact not written:\n%s", got.out())
		}
		if !w.exists(".team-context/playbooks/acme-deploy.md") {
			t.Errorf("playbook not written:\n%s", got.out())
		}
		if body := w.read(".team-context/materialized.md"); !strings.Contains(body, "deploy.acme.db-endpoint") {
			t.Errorf("fact did not reach materialized.md:\n%s", body)
		}
	})

	t.Run("is idempotent via the ledger", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		w.fakeClaude(proposal(okProposal))
		w.session("sess.jsonl", longSession()...)

		w.run("distill")
		second := w.run("distill")
		if !strings.Contains(second.stdout, "nothing to distill") {
			t.Errorf("ledger did not dedupe the session:\n%s", second.out())
		}
	})

	t.Run("--force re-distills", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		w.fakeClaude(proposal(okProposal))
		w.session("sess.jsonl", longSession()...)

		w.run("distill")
		forced := w.run("distill", "--force")
		if strings.Contains(forced.stdout, "nothing to distill") {
			t.Errorf("--force should re-distill, got:\n%s", forced.out())
		}
	})

	t.Run("--dry-run writes nothing", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		w.fakeClaude(proposal(okProposal))
		w.session("sess.jsonl", longSession()...)

		got := w.run("distill", "--dry-run")
		if got.code != 0 {
			t.Fatalf("exit=%d: %s", got.code, got.out())
		}
		if !strings.Contains(got.stdout, "deploy.acme.db-endpoint") {
			t.Errorf("dry run should print the proposal, got:\n%s", got.out())
		}
		if w.exists(".team-context/facts/deploy.acme.db-endpoint.md") {
			t.Error("dry run wrote a fact to disk")
		}
		// A dry run must also leave the session pending, not consume it.
		if got := w.run("status"); !strings.Contains(got.stdout, "pending sessions: 1") {
			t.Errorf("dry run consumed the session:\n%s", got.out())
		}
	})

	t.Run("--transcript targets one file", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		w.fakeClaude(proposal(okProposal))
		path := w.session("sess.jsonl", longSession()...)

		got := w.run("distill", "--transcript", path)
		if got.code != 0 {
			t.Fatalf("exit=%d: %s", got.code, got.out())
		}
		if !w.exists(".team-context/facts/deploy.acme.db-endpoint.md") {
			t.Errorf("fact not written:\n%s", got.out())
		}
	})

	t.Run("missing transcript is reported, not fatal", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		w.fakeClaude(proposal(okProposal))
		got := w.run("distill", "--transcript", filepath.Join(w.repo, "nope.jsonl"))
		if got.code != 0 {
			t.Errorf("a missing transcript should be skipped, not fatal: %s", got.out())
		}
		if !strings.Contains(got.stderr, "skip") {
			t.Errorf("want a skip notice on stderr, got: %s", got.out())
		}
	})
}

// The distiller is a language model: it will eventually return prose, half a
// JSON object, or nothing at all. None of that may corrupt the knowledge base.
func TestDistillSurvivesBadModelOutput(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
	}{
		{"empty output", ""},
		{"prose instead of json", "Sure! Here are the facts I found:\n- the db is on 6432"},
		{"envelope with prose result", proposal("I could not find anything durable.")},
		{"truncated json", proposal(`{"facts":[{"id":"a.b","confidence":"high"`)},
		{"facts is not an array", proposal(`{"facts":"lots"}`)},
		{"fact missing id", proposal(`{"facts":[{"confidence":"high","body":"something"}]}`)},
		{"fact with empty body", proposal(`{"facts":[{"id":"a.b","confidence":"high","body":""}]}`)},
		{"fact id with path traversal", proposal(`{"facts":[{"id":"../../etc/passwd","confidence":"high","body":"x"}]}`)},
		{"null proposal", proposal("null")},
		{"deeply nested garbage", proposal(`{"facts":[[[[[]]]]]}`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.run("init")
			w.fakeClaude(tc.stdout)
			w.session("sess.jsonl", longSession()...)

			got := w.run("distill")
			// Either a clean error or a clean no-op is acceptable; a panic,
			// a corrupted store, or an escaped write is not.
			if strings.Contains(got.out(), "panic:") {
				t.Fatalf("panicked on bad model output:\n%s", got.out())
			}
			if status := w.run("status"); strings.Contains(status.stdout, "BAD") {
				t.Errorf("bad model output corrupted the store:\n%s", status.out())
			}
			outside := filepath.Join(filepath.Dir(w.repo), "etc")
			if _, err := os.Stat(outside); err == nil {
				t.Error("a fact id escaped the knowledge directory")
			}
		})
	}
}

func TestDistillWhenClaudeFails(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	w.fakeClaudeFailing()
	w.session("sess.jsonl", longSession()...)

	got := w.run("distill")
	if strings.Contains(got.out(), "panic:") {
		t.Fatalf("panicked:\n%s", got.out())
	}
	if got.code == 0 {
		t.Errorf("a failing distiller should surface as an error, got:\n%s", got.out())
	}
	// The session must stay pending so the user can retry after fixing things.
	if s := w.run("status"); !strings.Contains(s.stdout, "pending sessions: 1") {
		t.Errorf("a failed distill consumed the session:\n%s", s.out())
	}
}

func TestStatusAndDoctor(t *testing.T) {
	t.Run("status on an empty store", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		got := w.run("status")
		if got.code != 0 {
			t.Fatalf("exit=%d: %s", got.code, got.out())
		}
		if !strings.Contains(got.stdout, "facts: 0") {
			t.Errorf("want an empty summary, got:\n%s", got.out())
		}
	})

	t.Run("status reports broken files without crashing", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		bad := filepath.Join(w.repo, ".team-context", "facts", "broken.md")
		if err := os.WriteFile(bad, []byte("this has no frontmatter at all"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := w.run("status")
		if got.code != 0 {
			t.Fatalf("status must survive a broken file, exit=%d: %s", got.code, got.out())
		}
		if !strings.Contains(got.stdout, "BAD") {
			t.Errorf("want the broken file named, got:\n%s", got.out())
		}
	})

	t.Run("doctor passes on a healthy repo", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		w.fakeClaude(proposal(okProposal))
		got := w.run("doctor")
		if got.code != 0 {
			t.Errorf("doctor failed on a healthy repo:\n%s", got.out())
		}
		if strings.Contains(got.stdout, "FAIL") {
			t.Errorf("doctor reported a failure:\n%s", got.out())
		}
	})

	t.Run("doctor fails before init", func(t *testing.T) {
		w := newWorld(t)
		w.fakeClaude(proposal(okProposal))
		got := w.run("doctor")
		if got.code == 0 {
			t.Errorf("doctor should fail before init:\n%s", got.out())
		}
		if !strings.Contains(got.stdout, "still init") {
			t.Errorf("doctor should suggest `still init`:\n%s", got.out())
		}
	})

	t.Run("doctor flags a missing claude CLI", func(t *testing.T) {
		w := newWorld(t)
		w.run("init")
		// no fakeClaude installed
		got := w.run("doctor")
		if !strings.Contains(got.stdout, "claude CLI") || !strings.Contains(got.stdout, "FAIL") {
			t.Errorf("doctor should flag the missing claude CLI:\n%s", got.out())
		}
	})
}

func TestUsage(t *testing.T) {
	w := newWorld(t)
	if got := w.run("--help"); got.code != 0 || !strings.Contains(got.stderr, "still distill") {
		t.Errorf("--help: exit=%d\n%s", got.code, got.out())
	}
	if got := w.run("bogus-command"); got.code != 2 {
		t.Errorf("unknown command: exit=%d want 2\n%s", got.code, got.out())
	}
	if got := w.runIn(w.repo, ""); got.code != 2 {
		t.Errorf("no args: exit=%d want 2\n%s", got.code, got.out())
	}
}
