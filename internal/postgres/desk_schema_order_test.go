package postgres

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// A provider that constrains output to a schema writes each object's keys in
// the order the schema lists them. Every action form must therefore list "op"
// first, or the forms cannot be told apart from their first key. This reads
// the schema as text on purpose: decoding it into a map would hide the order.
func TestSecretarySchemasListOpFirstInEveryAction(t *testing.T) {
	for name, schema := range map[string]json.RawMessage{"answer": secretaryOutputSchema, "selfcheck": secretaryCheckSchema} {
		if !json.Valid(schema) {
			t.Fatalf("%s schema is not valid JSON", name)
		}
		var doc struct {
			Properties struct {
				Actions struct {
					Items struct {
						AnyOf []struct {
							Properties json.RawMessage `json:"properties"`
						} `json:"anyOf"`
					} `json:"items"`
				} `json:"actions"`
			} `json:"properties"`
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(schema, &doc); err != nil {
			t.Fatal(name, err)
		}
		want := map[string]int{"answer": 6, "selfcheck": 7}[name]
		if len(doc.Properties.Actions.Items.AnyOf) != want {
			t.Fatalf("%s schema has %d action forms, expected %d", name, len(doc.Properties.Actions.Items.AnyOf), want)
		}
		for i, form := range doc.Properties.Actions.Items.AnyOf {
			dec := json.NewDecoder(bytes.NewReader(form.Properties))
			if _, err := dec.Token(); err != nil {
				t.Fatal(name, i, err)
			}
			first, err := dec.Token()
			if err != nil || first != "op" {
				t.Errorf("%s schema, action form %d: first key is %v, must be op", name, i, first)
			}
		}
		if !strings.Contains(strings.Join(doc.Required, ","), "memoryPlan") {
			t.Errorf("%s schema does not require memoryPlan", name)
		}
	}
}
