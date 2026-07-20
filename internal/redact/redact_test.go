package redact

import (
	"strings"
	"testing"
)

func TestTextScrubsSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
		gone string // substring that must NOT survive
	}{
		{"aws", "creds AKIAIOSFODNN7EXAMPLE ok", "AKIAIOSFODNN7EXAMPLE"},
		{"github", "use ghp_16C7e42F292c6912E7710c838347Ae178B4a here", "ghp_16C7e42F"},
		{"anthropic", "key sk-ant-api03-abcdefghijklmnopqrstuv", "sk-ant-api03"},
		{"assignment", `DB_PASSWORD = "hunter2hunter2hunter2"`, "hunter2"},
		{"url auth", "postgres://admin:s3cretpass@db.acme.com:5432/prod", "s3cretpass"},
		{"jwt", "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "dozjgNryP4J3jVmNHl0w5N"},
	}
	for _, tc := range cases {
		out, n := Text(tc.in)
		if n == 0 {
			t.Errorf("%s: nothing redacted in %q", tc.name, tc.in)
			continue
		}
		if strings.Contains(out, tc.gone) {
			t.Errorf("%s: secret survived: %q", tc.name, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("%s: no placeholder in %q", tc.name, out)
		}
	}
}

func TestTextKeepsContext(t *testing.T) {
	out, _ := Text("postgres://admin:s3cretpass@db.acme.com:5432/prod")
	if !strings.Contains(out, "db.acme.com") {
		t.Errorf("host should survive url-auth redaction: %q", out)
	}
	out, _ = Text(`api_key = "abcdefghijklmnop1234"`)
	if !strings.Contains(out, "api_key") {
		t.Errorf("key name should survive assignment redaction: %q", out)
	}
}

func TestTextLeavesNormalProseAlone(t *testing.T) {
	in := "Acme 生产库入口是 pgbouncer,端口 6432;直连 5432 会被安全组拦。部署要跑 make deploy-prod。"
	out, n := Text(in)
	if n != 0 || out != in {
		t.Errorf("normal prose was modified (%d): %q", n, out)
	}
}
