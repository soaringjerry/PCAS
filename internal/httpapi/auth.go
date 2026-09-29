package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// Authenticator is the replacement point for sessions and per-agent tokens.
// The initial server has one configured owner credential, never an X-User-ID.
type Authenticator interface {
	Authenticate(*http.Request) (memory.Scope, bool)
}

type OwnerToken struct {
	tokenHash [32]byte
	ownerID   memory.ID
}

func NewOwnerToken(token string, ownerID memory.ID) *OwnerToken {
	return &OwnerToken{tokenHash: sha256.Sum256([]byte(token)), ownerID: ownerID}
}

func (a *OwnerToken) Authenticate(r *http.Request) (memory.Scope, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return memory.Scope{}, false
	}
	hash := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(hash[:], a.tokenHash[:]) != 1 {
		return memory.Scope{}, false
	}
	return memory.Scope{OwnerID: a.ownerID, PrincipalID: "owner:" + string(a.ownerID), IsOwner: true}, true
}
