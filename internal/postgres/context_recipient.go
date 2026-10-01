package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// contextRecipientTx binds the actual server route. Credentials are neither
// fingerprinted nor returned. Manual selection must identify a configured
// destination; its client-provided model/route fields never grant authority.
func (s *Store) contextRecipientTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agentID, role string, manual *memory.Recipient) (memory.Recipient, error) {
	var out memory.Recipient
	if !oneOf(role, "secretary", "deputy", "manual") {
		return out, memory.ErrInvalid
	}
	agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), agentID)
	if err != nil || !agent.Enabled {
		return out, memory.ErrForbidden
	}
	providerID := agentID
	if agent.Channel == "manual" {
		if manual == nil || manual.Provider == "" {
			return out, memory.ErrInvalid
		}
		providerID = manual.Provider
	} else if manual != nil {
		return out, memory.ErrInvalid
	}
	p, ok := s.models.Get(providerID)
	if !ok {
		return out, memory.ErrUnavailable
	}
	model, account := p.Model, ""
	if p.Protocol == "siwc" && s.models.ChatGPT != nil {
		status, err := s.models.ChatGPT.Status(ctx) // Local configuration read, no network.
		if err != nil {
			return out, memory.ErrUnavailable
		}
		for _, a := range status.Accounts {
			if a.ClientID == status.Active {
				account = a.ClientID + "\x00" + a.Email
				if model == "" {
					model = a.Model
				}
			}
		}
	}
	// A subscription's unspecified, dynamically selected model cannot authorize
	// raw personal content as though its eventual recipient were known.
	if model == "" {
		return out, memory.ErrUnavailable
	}
	endpoint := strings.TrimRight(p.BaseURL, "/")
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return out, memory.ErrInvalid
		}
		u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
		endpoint = u.String()
	}
	hash := sha256.Sum256(asJSON([]string{p.ID, p.Protocol, model, endpoint, account}))
	out = memory.Recipient{PrincipalID: agentID, Role: role, Model: model, Provider: p.ID, Protocol: p.Protocol, Channel: agent.Channel, RouteFingerprint: fmt.Sprintf("%x", hash)}
	if agent.Channel == "manual" {
		if manual.PrincipalID != "" && manual.PrincipalID != out.PrincipalID || manual.Role != "" && manual.Role != out.Role || manual.Channel != "" && manual.Channel != out.Channel || manual.Model != "" && manual.Model != out.Model || manual.Protocol != "" && manual.Protocol != out.Protocol || manual.RouteFingerprint != "" && manual.RouteFingerprint != out.RouteFingerprint {
			return memory.Recipient{}, memory.ErrConflict
		}
	}
	return out, nil
}

func (s *Store) trustedTaskContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agentID, role string, hardScope memory.HardScope, manual *memory.Recipient) (memory.TrustedTaskContext, error) {
	out := memory.TrustedTaskContext{OwnerID: scope.OwnerID, Purpose: memory.KnowledgePurpose, Scope: hardScope, View: memory.VersionView{Mode: memory.Remember}, Now: time.Now().UTC(), MemoryBudget: memory.Budget{Candidates: 15, Tokens: 4000, Edges: 15, Hops: 1}, TotalInputTokens: 8000}
	if !hardScope.Valid() {
		return out, memory.ErrInvalid
	}
	var err error
	out.Recipient, err = s.contextRecipientTx(ctx, tx, scope, agentID, role, manual)
	if err != nil {
		return out, err
	}
	settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	out.Timezone = deskLocation(settings).String()
	return out, nil
}

func contextScopeForItem(item *workspace.Item) memory.HardScope {
	if item == nil {
		return memory.HardScope{Kind: memory.UnscopedContextScope}
	}
	project := item.ProjectID
	if item.Kind == "project" {
		project = item.ID
	}
	if project != "" {
		return memory.HardScope{Kind: memory.StudioContextScope, StudioID: memory.ID(project), IncludeGlobalConstraints: true}
	}
	return memory.HardScope{Kind: memory.UnscopedContextScope}
}
