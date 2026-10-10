package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsFileWithRetiredRoutingKeyLoadsAndDropsItOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(`{"text":{"base_url":"https://example.invalid/v1","model":"m","api_key":"k","input_cny_per_million":1,"output_cny_per_million":4},"decision":{"api_key":"retired"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := &Registry{SettingsPath: path}
	if status := r.ConnectionStatus("text"); status.Model != "m" || !status.KeyConfigured {
		t.Fatalf("a file with the retired key did not load: %+v", status)
	}
	if err := r.SaveConnection("text", Connection{BaseURL: "https://example.invalid/v1", Model: "m2", APIKey: "k", InputPrice: 1, OutputPrice: 4}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "retired") || !strings.Contains(string(data), "m2") {
		t.Fatal("the retired key stayed in the saved file", err)
	}
}
