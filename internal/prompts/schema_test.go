package prompts

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestSchemaBytesCannotChangeRegisteredContent(t *testing.T) {
	for name, schema := range schemas {
		original := schema.Bytes()
		expected := fmt.Sprintf("%x", sha256.Sum256(original))
		mutable := schema.Bytes()
		mutable[0] = '!'
		again, ok := GetSchema(name)
		if !ok || again.Hash() != expected || string(again.Bytes()) != string(original) || again.Name() != name {
			t.Fatal("caller changed registered schema", name)
		}
	}
	if _, ok := GetSchema("unregistered"); ok {
		t.Fatal("unknown schema was registered")
	}
	if (Schema{}).Bytes() != nil || (Schema{}).Name() != "" || (Schema{}).Hash() != "" {
		t.Fatal("absent schema has an identity or payload")
	}
}
