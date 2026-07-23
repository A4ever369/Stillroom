// Package text holds the one tokenizer the whole project shares.
//
// It exists because two places need to decide "are these two pieces of prose
// about the same thing": search ranking (internal/index) and near-duplicate
// detection (internal/distill). Two tokenizers would drift, and they would
// drift silently — a fact findable by search but invisible to the duplicate
// tripwire is exactly the failure that lets a knowledge base grow two copies
// of the same thing.
//
// Zero dependencies, like everything else here.
package text

import (
	"strings"
	"unicode"
)

// Tokens lowercases and splits on non-alphanumeric runs. CJK has no spaces, so
// CJK runs additionally emit character bigrams — enough to make Chinese prose
// comparable without pulling in a segmenter (zero-dependency rule).
//
// Tokens may repeat; callers that want a set should build one.
func Tokens(s string) []string {
	var out []string
	var cur []rune
	var cjk []rune

	flushWord := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	flushCJK := func() {
		for i, r := range cjk {
			out = append(out, string(r))
			if i+1 < len(cjk) {
				out = append(out, string(cjk[i:i+2]))
			}
		}
		cjk = cjk[:0]
	}

	for _, r := range strings.ToLower(s) {
		switch {
		case isCJK(r):
			flushWord()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			cur = append(cur, r)
		default:
			flushWord()
			flushCJK()
		}
	}
	flushWord()
	flushCJK()
	return out
}

// Set returns the distinct tokens of s.
func Set(s string) map[string]struct{} {
	toks := Tokens(s)
	out := make(map[string]struct{}, len(toks))
	for _, t := range toks {
		out[t] = struct{}{}
	}
	return out
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK unified ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // extension A
		(r >= 0x3040 && r <= 0x30FF) // kana
}
