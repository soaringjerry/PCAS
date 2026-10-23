package contextbuilder

import "unicode/utf8"

// Tail is the last n bytes of s, cut at the start of a character. A model
// input keeps the recent end of a text that is over its budget.
func Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}
