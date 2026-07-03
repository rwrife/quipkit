// Package match provides fuzzy ranking over quipkit snippets.
//
// It is a pure ranking layer with no I/O and no UI. Given a query string
// and a slice of snippets, Rank returns the snippets in relevance order.
//
// Ranking rules (best effort, kept simple on purpose):
//
//   - Empty/whitespace query: returns the input slice unchanged (original
//     order preserved).
//   - The query is fuzzy-matched (via github.com/sahilm/fuzzy) against
//     three per-snippet "haystacks" derived from the snippet: its title,
//     its tags joined with spaces, and its body.
//   - Each field's fuzzy score is multiplied by a per-field weight
//     (title > tags > body) and the maximum weighted score wins. A snippet
//     is included in the result if it matches on at least one field.
//   - Ties break on: higher raw score → shorter title → snippet ID
//     (ascending), so ordering is deterministic across runs.
package match

import (
	"strings"

	"github.com/rwrife/quipkit/internal/store"
	"github.com/sahilm/fuzzy"
)

// Field weights. Title is the strongest signal, then tags, then body.
// Tweak sparingly — the tests below encode the expected ordering.
const (
	weightTitle = 3.0
	weightTags  = 2.0
	weightBody  = 1.0

	// Exact-token bonuses reward "the query is a full word in this field",
	// which better matches user intuition than raw fuzzy edit-distance scores.
	// They stack on top of the fuzzy score for the winning field.
	bonusTitleToken = 1000.0
	bonusTagToken   = 800.0
	bonusBodyToken  = 200.0
)

// Rank returns the input snippets ordered by fuzzy relevance to query.
//
// If query is empty or whitespace, the input is returned unchanged so
// callers can use Rank as the single ordering entry point.
//
// Snippets that don't match on any field are omitted from the result.
func Rank(query string, in []store.Snippet) []store.Snippet {
	q := strings.TrimSpace(query)
	if q == "" {
		// Preserve caller's ordering (typically store.Load's stable ID sort).
		return in
	}
	if len(in) == 0 {
		return nil
	}

	scored := make([]scoredSnippet, 0, len(in))
	for idx, s := range in {
		score, ok := scoreSnippet(q, s)
		if !ok {
			continue
		}
		scored = append(scored, scoredSnippet{
			snip:   s,
			score:  score,
			origIx: idx,
		})
	}

	// Stable-ish sort: higher score first, then deterministic tiebreak.
	sortScored(scored)

	out := make([]store.Snippet, len(scored))
	for i, ss := range scored {
		out[i] = ss.snip
	}
	return out
}

// scoreSnippet returns the best weighted fuzzy score of q against the
// snippet's title/tags/body haystacks, and false if no field matched.
func scoreSnippet(q string, s store.Snippet) (float64, bool) {
	titleScore, titleOK := fuzzyScore(q, s.Title)
	tagsScore, tagsOK := fuzzyScore(q, strings.Join(s.Tags, " "))
	bodyScore, bodyOK := fuzzyScore(q, s.Body)

	if !titleOK && !tagsOK && !bodyOK {
		return 0, false
	}

	ql := strings.ToLower(q)

	best := 0.0
	if titleOK {
		score := float64(titleScore) * weightTitle
		if containsToken(strings.ToLower(s.Title), ql) {
			score += bonusTitleToken
		}
		best = maxFloat(best, score)
	}
	if tagsOK {
		score := float64(tagsScore) * weightTags
		if hasTagToken(s.Tags, ql) {
			score += bonusTagToken
		}
		best = maxFloat(best, score)
	}
	if bodyOK {
		score := float64(bodyScore) * weightBody
		if containsToken(strings.ToLower(s.Body), ql) {
			score += bonusBodyToken
		}
		best = maxFloat(best, score)
	}
	return best, true
}

// containsToken returns true if q appears in text as a whole "word", where
// word boundaries are anything that isn't a letter or a digit. Case-insensitive
// (both inputs should already be lowered by the caller).
func containsToken(text, q string) bool {
	if q == "" || text == "" {
		return false
	}
	for i := 0; i+len(q) <= len(text); i++ {
		if text[i:i+len(q)] != q {
			continue
		}
		if i > 0 && isWordByte(text[i-1]) {
			continue
		}
		end := i + len(q)
		if end < len(text) && isWordByte(text[end]) {
			continue
		}
		return true
	}
	return false
}

// hasTagToken returns true if any tag equals q (case-insensitive).
func hasTagToken(tags []string, q string) bool {
	for _, t := range tags {
		if strings.ToLower(t) == q {
			return true
		}
	}
	return false
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// fuzzyScore runs sahilm/fuzzy against a single haystack string and
// returns (score, matched). An empty haystack never matches.
func fuzzyScore(q, hay string) (int, bool) {
	if hay == "" {
		return 0, false
	}
	matches := fuzzy.Find(q, []string{hay})
	if len(matches) == 0 {
		return 0, false
	}
	// sahilm/fuzzy uses "higher = better".
	return matches[0].Score, true
}

type scoredSnippet struct {
	snip   store.Snippet
	score  float64
	origIx int
}

func sortScored(xs []scoredSnippet) {
	// Insertion sort is fine — snippet libraries are small (dozens to hundreds).
	// This keeps the ordering deterministic without leaning on sort.Stable's
	// comparator subtleties.
	for i := 1; i < len(xs); i++ {
		j := i
		for j > 0 && less(xs[j], xs[j-1]) {
			xs[j], xs[j-1] = xs[j-1], xs[j]
			j--
		}
	}
}

// less returns true if a should sort before b.
func less(a, b scoredSnippet) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	// Prefer shorter titles on ties (usually the tighter match).
	if len(a.snip.Title) != len(b.snip.Title) {
		return len(a.snip.Title) < len(b.snip.Title)
	}
	// Final deterministic tiebreak: snippet ID ascending.
	return a.snip.ID < b.snip.ID
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
