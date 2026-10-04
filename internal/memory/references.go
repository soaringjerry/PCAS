package memory

import "strings"

// A reading hint, not a claim that the referent has been resolved.
func ContextDependent(text string) bool {
	for _, token := range []string{"这位", "那位", "这名", "那名", "这个人", "那个人", "对方", "这件事", "那件事", "前者", "后者", "她"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	cleaned := strings.NewReplacer("其他", "", "他人", "").Replace(text)
	return strings.Contains(cleaned, "他")
}

func AmbiguousEntityName(name string) bool {
	name = strings.TrimSpace(name)
	for _, token := range []string{"这位", "那位", "这名", "那名", "这个", "那个"} {
		if strings.HasPrefix(name, token) {
			return true
		}
	}
	switch name {
	case "他", "她", "它", "他们", "她们", "它们", "对方", "前者", "后者":
		return true
	}
	return false
}
