package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
)

type agentCredential struct {
	hash      [32]byte
	principal string
}
type Credentials struct {
	owner  *OwnerToken
	agents []agentCredential
}

// A principal is bound by server configuration and can only read explicitly
// granted records. Provider API keys and inbound PCAS credentials are separate.
func LoadCredentials(token string, ownerID memory.ID, path string) (Authenticator, error) {
	a := &Credentials{owner: NewOwnerToken(token, ownerID)}
	if path == "" {
		return a, nil
	}
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read agent credentials configuration")
	}
	var entries []struct {
		Principal string `json:"principal"`
		TokenEnv  string `json:"token_env"`
	}
	if err := json.Unmarshal(file, &entries); err != nil {
		return nil, fmt.Errorf("invalid agent credentials configuration")
	}
	seen := map[string]bool{}
	hashes := map[[32]byte]bool{sha256.Sum256([]byte(token)): true}
	for _, entry := range entries {
		secret := os.Getenv(entry.TokenEnv)
		hash := sha256.Sum256([]byte(secret))
		if entry.Principal == "" || strings.HasPrefix(entry.Principal, "owner:") || seen[entry.Principal] || len(secret) < 32 || hashes[hash] {
			return nil, fmt.Errorf("agent credentials require unique principals and distinct tokens of at least 32 characters")
		}
		seen[entry.Principal] = true
		hashes[hash] = true
		a.agents = append(a.agents, agentCredential{hash: hash, principal: entry.Principal})
	}
	return a, nil
}
func (a *Credentials) Authenticate(r *http.Request) (memory.Scope, bool) {
	if scope, ok := a.owner.Authenticate(r); ok {
		return scope, true
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return memory.Scope{}, false
	}
	hash := sha256.Sum256([]byte(token))
	for _, agent := range a.agents {
		if subtle.ConstantTimeCompare(hash[:], agent.hash[:]) == 1 {
			return memory.Scope{OwnerID: a.owner.ownerID, PrincipalID: agent.principal}, true
		}
	}
	return memory.Scope{}, false
}
