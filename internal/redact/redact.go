// Package redact scrubs likely secrets from text before it leaves the
// machine or enters the shared knowledge repo.
//
// Design note (docs/design-v2.md §4.1): distillation is a CONCENTRATOR, not
// a sanitizer — distilled facts are exactly where credentials-shaped
// knowledge gathers. Redaction therefore runs on distiller OUTPUT (facts,
// playbooks) as well as on any transcript digest fed into prompts.
package redact

import "regexp"

const placeholder = "[REDACTED]"

type rule struct {
	name string
	re   *regexp.Regexp
}

// Patterns are intentionally conservative: false negatives are reviewed by a
// human in the PR anyway (§13), while false positives destroy fact bodies.
var rules = []rule{
	{"aws-access-key", regexp.MustCompile(`\b(A3T[A-Z0-9]|AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`)},
	{"private-key-block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)},
	{"github-token", regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{20,255}\b`)},
	{"slack-token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"anthropic-key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)},
	{"openai-key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
	{"bearer-header", regexp.MustCompile(`(?i)\b(authorization:\s*bearer)\s+[A-Za-z0-9._~+/=-]{16,}`)},
	{"url-basic-auth", regexp.MustCompile(`\b([a-z][a-z0-9+.-]*://[^/\s:@]+):([^@/\s]+)@`)},
	{"assignment", regexp.MustCompile(`(?i)([A-Za-z0-9_-]*(?:api[_-]?key|secret|token|passwd|password|credential)s?\s*[:=]\s*)["']?[A-Za-z0-9._~+/-]{12,}["']?`)},
}

// keepGroup maps rule name → capture group to preserve (rewriting only the
// secret part). Rules absent from this map are replaced wholesale.
var keepGroup = map[string]string{
	"bearer-header":  "$1 " + placeholder,
	"url-basic-auth": "$1:" + placeholder + "@",
	"assignment":     "$1" + placeholder,
}

// Text replaces likely secrets in s and reports how many replacements were
// made. Zero means the text passed through unchanged.
func Text(s string) (string, int) {
	total := 0
	for _, r := range rules {
		matches := r.re.FindAllStringIndex(s, -1)
		if len(matches) == 0 {
			continue
		}
		total += len(matches)
		if repl, ok := keepGroup[r.name]; ok {
			s = r.re.ReplaceAllString(s, repl)
		} else {
			s = r.re.ReplaceAllString(s, placeholder)
		}
	}
	return s, total
}
