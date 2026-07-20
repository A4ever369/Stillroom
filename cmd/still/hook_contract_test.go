package main

// Hard rule (CLAUDE.md): `still hook ...` must exit 0 silently on any problem —
// it is not allowed to break a user's session. That contract is absolute, so
// it gets an absolute test: whatever we feed it, exit 0 and say nothing.

import (
	"strings"
	"testing"
)

func TestHookNeverBreaksTheSession(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		stdin string
	}{
		{"empty stdin", []string{"hook", "session-end"}, ""},
		{"not json", []string{"hook", "session-end"}, "this is not json at all"},
		{"json but not an object", []string{"hook", "session-end"}, `["a","b"]`},
		{"object missing transcript_path", []string{"hook", "session-end"}, `{"cwd":"/tmp"}`},
		{"empty transcript_path", []string{"hook", "session-end"}, `{"transcript_path":""}`},
		{"transcript_path does not exist", []string{"hook", "session-end"}, `{"transcript_path":"/nope/gone.jsonl"}`},
		{"cwd does not exist", []string{"hook", "session-end"}, `{"transcript_path":"/t.jsonl","cwd":"/nope/nowhere"}`},
		{"wrong types", []string{"hook", "session-end"}, `{"transcript_path":42,"cwd":true}`},
		{"truncated json", []string{"hook", "session-end"}, `{"transcript_path":"/t.jsonl"`},
		{"huge payload", []string{"hook", "session-end"}, `{"transcript_path":"` + strings.Repeat("x", 2<<20) + `"}`},
		{"nul bytes", []string{"hook", "session-end"}, "{\"transcript_path\":\"/t\x00.jsonl\"}"},
		{"unknown hook name", []string{"hook", "session-start"}, `{"transcript_path":"/t.jsonl"}`},
		{"no hook name", []string{"hook"}, `{"transcript_path":"/t.jsonl"}`},
		{"extra args", []string{"hook", "session-end", "--verbose"}, `{"transcript_path":"/t.jsonl"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.run("init")
			got := w.runIn(w.repo, tc.stdin, tc.args...)
			if got.code != 0 {
				t.Errorf("exit = %d, want 0 (hook must never fail a session)\noutput: %s", got.code, got.out())
			}
			if out := got.out(); out != "" {
				t.Errorf("hook must stay silent, printed:\n%s", out)
			}
		})
	}
}

// The hook is also called from repos that never opted in — that is the common
// case for anyone with the plugin installed globally. It must be a no-op, not
// an error, and must not create anything.
func TestHookInUninitializedRepoIsNoOp(t *testing.T) {
	w := newWorld(t)
	path := w.session("sess.jsonl", longSession()...)

	got := w.runIn(w.repo, `{"transcript_path":"`+path+`","cwd":"`+w.repo+`"}`, "hook", "session-end")
	if got.code != 0 || got.out() != "" {
		t.Fatalf("exit=%d out=%q, want silent success", got.code, got.out())
	}
	if w.exists(".team-context") {
		t.Error("hook created .team-context/ in a repo that never opted in")
	}
}

// The happy path: an opted-in repo gets the transcript enqueued, and distill
// then consumes it. This is the only path where the hook has visible effect.
func TestHookEnqueuesForOptedInRepo(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	path := w.session("sess.jsonl", longSession()...)

	got := w.runIn(w.repo, `{"transcript_path":"`+path+`","cwd":"`+w.repo+`"}`, "hook", "session-end")
	if got.code != 0 || got.out() != "" {
		t.Fatalf("exit=%d out=%q, want silent success", got.code, got.out())
	}

	status := w.run("status")
	if !strings.Contains(status.stdout, "pending sessions: 1") {
		t.Errorf("queued session not visible in status:\n%s", status.out())
	}
}

// Enqueueing the same transcript repeatedly must stay idempotent — a user who
// resumes a session several times should not distill it several times.
func TestHookEnqueueIsIdempotent(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	path := w.session("sess.jsonl", longSession()...)
	payload := `{"transcript_path":"` + path + `","cwd":"` + w.repo + `"}`

	for i := 0; i < 3; i++ {
		w.runIn(w.repo, payload, "hook", "session-end")
	}
	if got := w.run("status"); !strings.Contains(got.stdout, "pending sessions: 1") {
		t.Errorf("want 1 pending session after 3 enqueues:\n%s", got.out())
	}
}
