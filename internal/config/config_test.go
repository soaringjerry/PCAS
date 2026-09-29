package config

import (
	"strings"
	"testing"
)

func TestCredentialsRequiredOnlyForAPI(t *testing.T) {
	t.Setenv("PCAS_DATABASE_URL", "postgres://localhost/test")
	t.Setenv("PCAS_OWNER_ID", "")
	t.Setenv("PCAS_API_TOKEN", "")
	for _, mode := range []string{"worker", "migrate"} {
		if _, err := Load(mode); err != nil {
			t.Fatal(mode, err)
		}
	}
	if _, err := Load("serve"); err == nil {
		t.Fatal("API accepted missing owner")
	}
	t.Setenv("PCAS_OWNER_ID", "2e194d58-849d-45b7-a4ce-bfe4d53c063d")
	for _, token := range []string{"", "short", "replace-with-random-secret-at-least-32-characters"} {
		t.Setenv("PCAS_API_TOKEN", token)
		if _, err := Load("serve"); err == nil {
			t.Fatal("API accepted an unset or placeholder credential")
		}
	}
	t.Setenv("PCAS_API_TOKEN", strings.Repeat("a", 64))
	if _, err := Load("serve"); err != nil {
		t.Fatal(err)
	}
}
