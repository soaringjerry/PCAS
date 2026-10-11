package comparison

import "unicode"

// NamedEntity is what the keeper rule reads of one side of an entity pair.
type NamedEntity struct {
	Type        string
	Name        string
	MemoryCount int
}

// KeepIndex decides which of two names of one entity is kept, 0 or 1.
// A topic gives way to a place or an organization. Otherwise the side with
// more memories is kept, then the side with a Chinese name. When neither
// rule decides, the model's choice in fallback is used.
func KeepIndex(a, b NamedEntity, fallback int) int {
	if a.Type != b.Type {
		if a.Type == "topic" {
			return 1
		}
		return 0
	}
	if a.MemoryCount != b.MemoryCount {
		if a.MemoryCount > b.MemoryCount {
			return 0
		}
		return 1
	}
	if HasChinese(a.Name) != HasChinese(b.Name) {
		if HasChinese(a.Name) {
			return 0
		}
		return 1
	}
	return fallback
}

// HasChinese reports whether a name contains a Chinese character.
func HasChinese(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
