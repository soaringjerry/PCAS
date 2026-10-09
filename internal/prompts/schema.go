package prompts

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed *.json
var schemaFiles embed.FS

// Schema keeps the registered identity and original bytes immutable.
// Bytes returns a copy; JSON re-encoding would change required field order.
type Schema struct{ definition Definition }

func (s Schema) Name() string { return s.definition.Name() }
func (s Schema) Hash() string { return s.definition.Hash() }
func (s Schema) Bytes() json.RawMessage {
	if s.Name() == "" {
		return nil
	}
	return json.RawMessage(s.definition.Text())
}

var schemas = loadSchemas()

func loadSchemas() map[string]Schema {
	entries, err := schemaFiles.ReadDir(".")
	if err != nil {
		panic(err)
	}
	result := make(map[string]Schema, len(entries))
	for _, entry := range entries {
		data, err := schemaFiles.ReadFile(entry.Name())
		if err != nil {
			panic(err)
		}
		var schema map[string]json.RawMessage
		if json.Unmarshal(data, &schema) != nil || schema == nil {
			panic("invalid registered schema: " + entry.Name())
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		result[name] = Schema{definition: Definition{name: name, text: string(data), hash: fmt.Sprintf("%x", sha256.Sum256(data))}}
	}
	return result
}

func GetSchema(name string) (Schema, bool) { schema, ok := schemas[name]; return schema, ok }

func MustSchema(name string) Schema {
	schema, ok := GetSchema(name)
	if !ok {
		panic("unregistered schema: " + name)
	}
	return schema
}
