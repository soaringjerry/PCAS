// Package retrieval owns the memory retrieval rules that need no storage:
// the budget of a recall and the choice of the excerpt of a source.
package retrieval

import (
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// NormalizeBudget checks the budget of a recall and fills its absent limits:
// 30 candidates, 30 edges, 4,000 tokens, and one hop.
func NormalizeBudget(b memory.Budget) (memory.Budget, error) {
	if b.Candidates < 0 || b.Candidates > 1000 || b.Edges < 0 || b.Edges > 1000 || b.Tokens < 0 || b.Tokens > 32000 || b.Hops < 0 || b.Hops > 3 {
		return b, memory.ErrInvalid
	}
	if b.Candidates == 0 {
		b.Candidates = 30
	}
	if b.Edges == 0 {
		b.Edges = 30
	}
	if b.Tokens == 0 {
		b.Tokens = 4000
	}
	if b.Hops == 0 {
		b.Hops = 1
	}
	return b, nil
}

// NewCoverage is the coverage of a recall that left nothing out.
func NewCoverage() memory.Coverage {
	return memory.Coverage{Complete: true, Gaps: []string{}, PendingSources: []memory.ID{}}
}

// MatchedExcerpt keeps the answer-bearing span even while chunking is pending.
func MatchedExcerpt(text, query string, tokens []string, limit int) string {
	start, end := MatchedExcerptRange(text, query, tokens, limit)
	return string([]rune(text)[start:end])
}

// MatchedExcerptRange returns the part of a text, of limit characters at
// most, that starts shortly before the first place where the query or one of
// its words occurs.
func MatchedExcerptRange(text, query string, tokens []string, limit int) (int, int) {
	runes := []rune(text)
	if len(runes) <= limit {
		return 0, len(runes)
	}
	lower := strings.ToLower(text)
	at := -1
	if query != "" {
		at = strings.Index(lower, strings.ToLower(query))
	}
	if at < 0 {
		for _, token := range tokens {
			if len([]rune(token)) > 1 {
				if i := strings.Index(lower, strings.ToLower(token)); i >= 0 {
					at = i
					break
				}
			}
		}
	}
	start := 0
	if at >= 0 {
		start = len([]rune(lower[:at])) - min(200, limit/4)
		if start < 0 {
			start = 0
		}
	}
	if start+limit > len(runes) {
		start = len(runes) - limit
	}
	return start, start + limit
}

// SourceExcerpt returns the part of a source text that says most about the
// query, with a mark where text was left out.
// Leave room for the truncation mark within the per-excerpt rune budget.
func SourceExcerpt(text, query string, tokens []string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	start, end := SourceExcerptRange(text, query, tokens, limit-2)
	excerpt := string(runes[start:end])
	if start > 0 {
		excerpt = "…" + excerpt
	}
	if end < len(runes) {
		excerpt += "…"
	}
	return excerpt
}

// SourceExcerptRange scores windows anchored before each hit by distinct query words. Merge each word's
// matching window intervals so repeated occurrences only contribute one vote.
func SourceExcerptRange(text, query string, tokens []string, limit int) (int, int) {
	lower := strings.ToLower(text)
	if query != "" && strings.Contains(lower, strings.ToLower(query)) {
		return MatchedExcerptRange(text, query, tokens, limit)
	}
	length := len([]rune(text))
	if length <= limit {
		return 0, length
	}
	// Substring searches use bytes; all window bounds use Unicode characters.
	positions := make([]int, len(lower)+1)
	character := 0
	for byteIndex := range lower {
		positions[byteIndex] = character
		character++
	}
	positions[len(lower)] = character
	lastStart := length - limit
	votes := make([]int, lastStart+2)
	candidates := make([]bool, lastStart+1)
	prefix := min(200, limit/4)
	seen := map[string]bool{}
	matches := 0
	for _, token := range tokens {
		token = strings.ToLower(token)
		width := len([]rune(token))
		if width <= 1 || seen[token] {
			continue
		}
		seen[token] = true
		left, right := -1, -1
		for offset := 0; offset < len(lower); {
			found := strings.Index(lower[offset:], token)
			if found < 0 {
				break
			}
			at := offset + found
			offset = at + 1
			matches++
			candidates[min(max(0, positions[at]-prefix), lastStart)] = true
			first := max(0, positions[at]+width-limit)
			last := min(positions[at], lastStart)
			if first > last {
				continue
			}
			if left < 0 {
				left, right = first, last
			} else if first <= right+1 {
				right = max(right, last)
			} else {
				votes[left]++
				votes[right+1]--
				left, right = first, last
			}
		}
		if left >= 0 {
			votes[left]++
			votes[right+1]--
		}
	}
	// Keep the existing amount of preceding context for a single hit.
	if matches == 1 {
		return MatchedExcerptRange(text, query, tokens, limit)
	}
	start, best, score := 0, -1, 0
	for candidate := 0; candidate <= lastStart; candidate++ {
		score += votes[candidate]
		if candidates[candidate] && score > best {
			start, best = candidate, score
		}
	}
	return start, start + limit
}
