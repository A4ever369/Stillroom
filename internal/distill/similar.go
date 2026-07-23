package distill

import (
	"math"
	"sort"
	"strings"

	"github.com/A4ever369/Stillroom/internal/ir"
	"github.com/A4ever369/Stillroom/internal/text"
)

// similarityThreshold: idf-weighted token Jaccard above this flags two fact
// bodies as probable duplicates filed under different ids. This is the
// PR-time tripwire, not the real entity-resolution answer (design-v2 §10
// keeps that open for research).
//
// The value is measured, not guessed. Distilling 21 real sessions across six
// repos (2026-07-23) produced 210 facts containing genuine duplicates, which
// gave this a ground truth to be tuned against. Over that corpus:
//
//	rune-bigram Jaccard @ 0.55 (previous)   flagged  3 pairs
//	idf-weighted token Jaccard @ 0.18       flagged 26 pairs
//
// and the pairs the old signal missed include hand-verified duplicates such as
// two facts about the same build failure, and the same knowledge re-filed
// under a different id by a later session.
//
// Rune bigrams cannot discriminate English technical prose: unrelated facts in
// one project share so much character-level material that their scores land in
// the same band as real duplicates, so no threshold separates them — raising
// it loses true positives, lowering it floods the output. Weighting whole
// tokens by how rare they are in the knowledge base separates the two
// populations by roughly an order of magnitude, and 0.18 sits in the gap with
// headroom above the highest non-duplicate observed (0.149).
//
// The knowledge bases this was tuned on are private, so the regression tests
// in similar_test.go reproduce the observed duplicate *shapes* with invented
// wording rather than the real bodies.
const similarityThreshold = 0.18

// SimilarExisting returns ids of existing facts that look like near-duplicates
// of f. Same-id comparisons are skipped: a newer observation of the same key
// is supersession, not duplication.
//
// Two independent signals, because they fail in different places:
//
//   - Identical id token sets. "discovery.delete-environment.cascade" and
//     "discovery.environment-delete.cascade" are the same words in a different
//     order — observed repeatedly in real distillation output, since the model
//     re-derives an id per session. Zero false positives by construction, and
//     it fires even when the two bodies are worded very differently.
//   - Body similarity above the threshold, for duplicates that chose genuinely
//     different words for the id.
//
// Results are sorted so the same inputs always produce the same output
// (determinism rule).
func SimilarExisting(f ir.Fact, existing []ir.Fact) []string {
	bodyTokens := text.Set(f.Body)
	idTokens := idTokenSet(f.ID)

	// Document frequency over the existing knowledge base only — deliberately
	// NOT including f. Counting f would make every token it shares with a
	// candidate appear in at least two documents while its unique tokens appear
	// in one, so in a small knowledge base the *shared* words would be weighted
	// below the unrelated ones and a real duplicate would score near zero.
	//
	// Excluding f also gives the right degenerate behaviour: with a single
	// existing fact every weight is equal, and the score falls back to plain
	// token Jaccard, which is the most that can be said when there is no corpus
	// to learn word rarity from.
	docs := make([]map[string]struct{}, len(existing))
	df := make(map[string]int, len(bodyTokens)*2)
	for i, e := range existing {
		docs[i] = text.Set(e.Body)
		for tok := range docs[i] {
			df[tok]++
		}
	}
	n := len(existing)

	var hits []string
	for i, e := range existing {
		if e.ID == f.ID {
			continue
		}
		if len(idTokens) > 0 && sameTokenSet(idTokens, idTokenSet(e.ID)) {
			hits = append(hits, e.ID)
			continue
		}
		if len(bodyTokens) == 0 {
			continue
		}
		if weightedJaccard(bodyTokens, docs[i], df, n) >= similarityThreshold {
			hits = append(hits, e.ID)
		}
	}
	sort.Strings(hits)
	return hits
}

// weightedJaccard is Jaccard with each token weighted by its inverse document
// frequency. Smoothed so the weight is always positive: without smoothing, a
// token present in every document would contribute zero to both sides and two
// short, identical facts in a two-fact knowledge base would score 0/0.
func weightedJaccard(a, b map[string]struct{}, df map[string]int, n int) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	idf := func(tok string) float64 {
		d := df[tok]
		if d < 1 {
			d = 1
		}
		return math.Log(float64(n+1) / float64(d))
	}
	var inter, union float64
	for tok := range a {
		w := idf(tok)
		union += w
		if _, ok := b[tok]; ok {
			inter += w
		}
	}
	for tok := range b {
		if _, ok := a[tok]; !ok {
			union += idf(tok)
		}
	}
	if union == 0 {
		return 0
	}
	return inter / union
}

// idTokenSet splits a semantic key into its parts. "deploy.acme.db-endpoint"
// and "deploy.acme.endpoint-db" produce the same set — which is the point.
func idTokenSet(id string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.FieldsFunc(id, func(r rune) bool { return r == '.' || r == '-' || r == '_' }) {
		if part != "" {
			out[strings.ToLower(part)] = struct{}{}
		}
	}
	return out
}

func sameTokenSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
