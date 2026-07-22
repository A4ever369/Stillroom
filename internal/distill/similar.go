package distill

import (
	"strings"

	"github.com/A4ever369/Stillroom/internal/ir"
)

// similarityThreshold: rune-bigram Jaccard above this flags two fact bodies
// as probable duplicates under different ids. Deliberately cheap and
// dependency-free — this is the PR-time tripwire, not the real
// entity-resolution answer (design-v2 §10 keeps that open for research).
const similarityThreshold = 0.55

// SimilarExisting returns ids of existing facts whose bodies look like
// near-duplicates of f. Same-id comparisons are skipped: a newer observation
// of the same key is supersession, not duplication.
func SimilarExisting(f ir.Fact, existing []ir.Fact) []string {
	fb := bigrams(f.Body)
	if len(fb) == 0 {
		return nil
	}
	var hits []string
	for _, e := range existing {
		if e.ID == f.ID {
			continue
		}
		if jaccard(fb, bigrams(e.Body)) >= similarityThreshold {
			hits = append(hits, e.ID)
		}
	}
	return hits
}

// bigrams builds a rune-bigram set. Rune-level (not word-level) so it works
// for Chinese prose, identifiers, and mixed text alike.
func bigrams(s string) map[string]struct{} {
	rs := []rune(normalize(s))
	out := make(map[string]struct{}, len(rs))
	for i := 0; i+1 < len(rs); i++ {
		out[string(rs[i:i+2])] = struct{}{}
	}
	return out
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	return float64(inter) / float64(union)
}
