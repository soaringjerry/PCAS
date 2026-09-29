package memory

import (
	"strings"
	"unicode"
)

// SearchTokens indexes CJK unigrams and adjacent bigrams along with Latin
// words. Exact raw-text recall remains available while indexing is pending.
func SearchTokens(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	var word []rune
	var previous rune
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	flush := func() { add(string(word)); word = nil }
	for _, r := range strings.ToLower(text) {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			flush()
			add(string(r))
			if previous != 0 {
				add(string([]rune{previous, r}))
			}
			previous = r
			continue
		}
		previous = 0
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}
