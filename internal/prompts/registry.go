// Package prompts owns the registered instructions used by migrated workflows.
package prompts

import (
	"crypto/sha256"
	"embed"
	"fmt"
)

//go:embed *.txt
var files embed.FS

// Definition keeps its text and hash together. Callers cannot change either.
type Definition struct {
	name string
	text string
	hash string
}

func (d Definition) Name() string { return d.name }
func (d Definition) Text() string { return d.text }
func (d Definition) Hash() string { return d.hash }

var registry = load()

func load() map[string]Definition {
	components := map[string][]string{
		"structured-extraction":   {"extraction-base", "reference-extraction", "structured-extraction"},
		"conversation-extraction": {"conversation-extraction", "reference-extraction"},
		"assistant":               {"assistant", "memory-trust"},
		"deputy":                  {"assistant", "memory-trust", "deputy"},
		"secretary":               {"assistant", "memory-trust", "line-break", "recall-date", "secretary"},
		"memory-reader":           {"assistant", "memory-trust", "memory-reader"},
		"document-revise":         {"assistant", "memory-trust", "deputy", "document-revise"},
		"secretary-selfcheck":     {"assistant", "memory-trust", "line-break", "recall-date", "secretary", "secretary-selfcheck"},
		"deputy-selfcheck":        {"assistant", "memory-trust", "deputy", "deputy-selfcheck"},
	}
	entries, err := files.ReadDir(".")
	if err != nil {
		panic(err)
	}
	result := make(map[string]Definition, len(entries))
	for _, entry := range entries {
		name := entry.Name()[:len(entry.Name())-4]
		parts := components[name]
		if len(parts) == 0 {
			parts = []string{name}
		}
		text := ""
		for _, part := range parts {
			data, err := files.ReadFile(part + ".txt")
			if err != nil {
				panic(err)
			}
			text += string(data)
		}
		result[name] = Definition{name: name, text: text, hash: fmt.Sprintf("%x", sha256.Sum256([]byte(text)))}
	}
	return result
}

func Get(name string) (Definition, bool) { d, ok := registry[name]; return d, ok }

// Must is for static wiring. An unknown name is a configuration error.
func Must(name string) Definition {
	d, ok := Get(name)
	if !ok {
		panic("unregistered prompt: " + name)
	}
	return d
}

// FromText supports the temporary background adapter. Remove it when that
// adapter's named callers pass Definition values directly.
func FromText(text string) (Definition, bool) {
	for _, d := range registry {
		if d.text == text {
			return d, true
		}
	}
	return Definition{}, false
}
