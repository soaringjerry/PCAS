package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
)

type Config struct {
	DatabaseURL string
	HTTPAddress string
	APIToken    string
	OwnerID     memory.ID
	PublicURL   string
}

func Load(command string) (Config, error) {
	c := Config{DatabaseURL: os.Getenv("PCAS_DATABASE_URL"), HTTPAddress: os.Getenv("PCAS_HTTP_ADDR"), APIToken: os.Getenv("PCAS_API_TOKEN"), OwnerID: memory.ID(os.Getenv("PCAS_OWNER_ID"))}
	c.PublicURL = strings.TrimRight(os.Getenv("PCAS_PUBLIC_URL"), "/")
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("PCAS_PUBLIC_URL must be an HTTP(S) origin without path")
		}
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("PCAS_DATABASE_URL is required")
	}
	if c.HTTPAddress == "" {
		c.HTTPAddress = "127.0.0.1:8090"
	}
	if command == "serve" {
		if !c.OwnerID.Valid() {
			return c, fmt.Errorf("PCAS_OWNER_ID must be a nonzero UUID")
		}
		if len(c.APIToken) < 32 || strings.Contains(c.APIToken, "replace-with") || strings.TrimSpace(c.APIToken) != c.APIToken {
			return c, fmt.Errorf("PCAS_API_TOKEN must be a generated secret of at least 32 characters")
		}
	}
	return c, nil
}
