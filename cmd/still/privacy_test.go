package main

// Privacy invariant (CLAUDE.md): distiller output is redacted before it
// touches disk. Distillation CONCENTRATES knowledge, so it is exactly where
// credential-shaped strings gather — the unit tests in internal/redact prove
// the patterns work, this proves they are actually wired into the write path.
//
// The assertion is deliberately blunt: after distilling, no file anywhere
// under the repo may contain the secret. Not the fact, not the playbook, not
// the materialized output, not the ledger.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each secret is written into a distiller proposal by the fake `claude`, so
// it enters through the exact path a real model's output would.
var secretCorpus = []struct {
	name   string
	secret string
}{
	{"aws access key", "AKIAIOSFODNN7EXAMPLE"},
	{"github token", "ghp_16CharsAndThenSomeMoreCharacters00"},
	{"slack token", "xoxb-123456789012-abcdefghijkl"},
	{"anthropic key", "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"},
	{"openai key", "sk-abcdefghijklmnopqrstuvwxyz0123456789"},
	{"jwt", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"},
	{"password assignment", `DB_PASSWORD = "hunter2hunter2hunter2"`},
	{"api key assignment", "api_key: abcdef0123456789abcdef"},
	{"basic auth url", "postgres://admin:s3cr3tpassw0rd@db.internal:6432/acme"},
	{"bearer header", "Authorization: Bearer abcdef0123456789abcdef0123456789"},
}

func TestSecretsNeverReachDisk(t *testing.T) {
	for _, tc := range secretCorpus {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.run("init")
			w.session("sess.jsonl", longSession()...)

			// The model dutifully repeats the secret in a fact body and a
			// playbook — the realistic failure mode, not a contrived one.
			body := "The connection detail is " + tc.secret + " — use it for prod."
			w.fakeClaude(proposal(`{"facts":[{"id":"deploy.acme.creds","confidence":"high","body":` +
				jsonString(body) + `}],"playbook":{"id":"acme-deploy","title":"Acme deploy","body":` +
				jsonString("## Steps\n1. export "+tc.secret) + `}}`))

			got := w.run("distill")
			if got.code != 0 {
				t.Fatalf("distill failed: %s", got.out())
			}

			// The distinguishing part of the secret must be gone. Compare on
			// the high-entropy tail so a pattern that keeps the variable name
			// (by design, e.g. `DB_PASSWORD = [REDACTED]`) still passes.
			needle := entropicPart(tc.secret)
			if leaked := grepTree(t, w.repo, needle); len(leaked) > 0 {
				t.Errorf("secret %q leaked into:\n  %s", needle, strings.Join(leaked, "\n  "))
			}
			if !strings.Contains(got.stdout, "redacted") {
				t.Errorf("distill did not report redacting anything:\n%s", got.out())
			}
		})
	}
}

// A session digest containing secrets must not leak either — the digest is
// what gets handed to the distiller, and the fact bodies quote from it.
func TestSecretsInTranscriptDoNotReachDisk(t *testing.T) {
	w := newWorld(t)
	w.run("init")
	secret := "AKIAIOSFODNN7EXAMPLE"
	lines := longSession()
	lines[3] = `{"type":"assistant","message":{"content":[{"type":"text","text":"the key is ` + secret + ` keep it safe"}]}}`
	w.session("sess.jsonl", lines...)

	// A model that echoes its input back verbatim is the worst case.
	w.fakeClaude(proposal(`{"facts":[{"id":"deploy.acme.creds","confidence":"high","body":` +
		jsonString("credentials seen in session: "+secret) + `}]}`))

	if got := w.run("distill"); got.code != 0 {
		t.Fatalf("distill failed: %s", got.out())
	}
	if leaked := grepTree(t, w.repo, secret); len(leaked) > 0 {
		t.Errorf("transcript secret leaked into:\n  %s", strings.Join(leaked, "\n  "))
	}
}

// entropicPart returns the segment of a secret that must never survive: for
// assignments and URLs, the value after the last delimiter; otherwise the
// whole string.
func entropicPart(secret string) string {
	for _, sep := range []string{`= "`, ": ", ":"} {
		if i := strings.LastIndex(secret, sep); i >= 0 {
			tail := strings.Trim(secret[i+len(sep):], `"@/`)
			if len(tail) >= 12 {
				if at := strings.Index(tail, "@"); at > 0 {
					tail = tail[:at]
				}
				return tail
			}
		}
	}
	return secret
}

// grepTree returns every file under root whose contents contain needle.
func grepTree(t *testing.T, root, needle string) []string {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(data), needle) {
			rel, _ := filepath.Rel(root, path)
			hits = append(hits, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hits
}
