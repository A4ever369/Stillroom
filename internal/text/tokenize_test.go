package text

import (
	"reflect"
	"strings"
	"testing"
)

func TestTokensSplitsLatinOnNonAlphanumeric(t *testing.T) {
	got := Tokens("Use pgvector/pgvector:pg16 — NOT postgres:16!")
	want := []string{"use", "pgvector", "pgvector", "pg16", "not", "postgres", "16"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokens() = %v, want %v", got, want)
	}
}

// The reason this package exists: CJK has no spaces, so a word tokenizer would
// collapse a whole Chinese sentence into one token and make it uncomparable.
// Characters plus their bigrams keep Chinese knowledge searchable and
// deduplicable without a segmenter.
func TestTokensEmitsCJKCharactersAndBigrams(t *testing.T) {
	got := Tokens("数据库")
	want := []string{"数", "数据", "据", "据库", "库"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokens() = %v, want %v", got, want)
	}
}

// A single CJK character has no bigram but must still produce a token.
func TestTokensHandlesLoneCJKCharacter(t *testing.T) {
	if got := Tokens("库"); !reflect.DeepEqual(got, []string{"库"}) {
		t.Errorf("Tokens() = %v, want [库]", got)
	}
}

// Mixed text is the common real case: a Chinese sentence quoting an identifier.
func TestTokensHandlesMixedScript(t *testing.T) {
	got := Tokens("重启 pm-server 会中断 pipeline")
	for _, want := range []string{"重", "重启", "启", "pm", "server", "中", "中断", "断", "pipeline"} {
		if !contains(got, want) {
			t.Errorf("Tokens() = %v, missing %q", got, want)
		}
	}
	// The Latin run must not be glued onto a CJK bigram.
	for _, tok := range got {
		if strings.ContainsAny(tok, "abcdefghijklmnopqrstuvwxyz") && strings.ContainsAny(tok, "重启中断") {
			t.Errorf("token %q mixes scripts", tok)
		}
	}
}

func TestTokensOnEmptyAndPunctuationOnly(t *testing.T) {
	for _, in := range []string{"", "   ", "--- !!! ///"} {
		if got := Tokens(in); len(got) != 0 {
			t.Errorf("Tokens(%q) = %v, want none", in, got)
		}
	}
}

// Set is the deduplicated view the similarity code compares against.
func TestSetDeduplicates(t *testing.T) {
	got := Set("build build BUILD fails")
	if len(got) != 2 {
		t.Errorf("Set() = %v, want 2 distinct tokens", got)
	}
	if _, ok := got["build"]; !ok {
		t.Errorf("Set() = %v, want it lowercased", got)
	}
}

// Same input, same output — both callers depend on this for determinism.
func TestTokensIsDeterministic(t *testing.T) {
	const in = "重启前必须备份 projects.db,否则 migration 不可逆"
	first := Tokens(in)
	for i := 0; i < 20; i++ {
		if !reflect.DeepEqual(Tokens(in), first) {
			t.Fatalf("run %d differs from the first", i)
		}
	}
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
