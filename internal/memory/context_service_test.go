package memory

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type phase2ContextServicePorts interface {
	SourceAuthorizer
	SourceScopeEditor
}

type phase2ServiceCall struct {
	Method        string
	Context       context.Context
	Scope         Scope
	ID            ID
	Authorization *SourceAuthorizationRequest
	Assignment    *SourceScopeRequest
}

type phase2ContextServiceRepo struct {
	recordingRepo // Existing Sources-only fake; no new ingestion parser or bypass.
	Calls         []phase2ServiceCall
	List          []SourceAuthorization
	Authorization SourceAuthorizationResult
	Assignment    SourceScopeResult
	Err           error
}

func (r *phase2ContextServiceRepo) SourceAuthorizations(ctx context.Context, scope Scope, id ID) ([]SourceAuthorization, error) {
	r.Calls = append(r.Calls, phase2ServiceCall{Method: "list_authorizations", Context: ctx, Scope: scope, ID: id})
	return r.List, r.Err
}
func (r *phase2ContextServiceRepo) SetSourceAuthorization(ctx context.Context, scope Scope, in SourceAuthorizationRequest) (SourceAuthorizationResult, error) {
	r.Calls = append(r.Calls, phase2ServiceCall{Method: "set_authorization", Context: ctx, Scope: scope, Authorization: &in})
	return r.Authorization, r.Err
}
func (r *phase2ContextServiceRepo) SourceScope(ctx context.Context, scope Scope, id ID) (SourceScopeResult, error) {
	r.Calls = append(r.Calls, phase2ServiceCall{Method: "get_scope", Context: ctx, Scope: scope, ID: id})
	return r.Assignment, r.Err
}
func (r *phase2ContextServiceRepo) SetSourceScope(ctx context.Context, scope Scope, in SourceScopeRequest) (SourceScopeResult, error) {
	r.Calls = append(r.Calls, phase2ServiceCall{Method: "set_scope", Context: ctx, Scope: scope, Assignment: &in})
	return r.Assignment, r.Err
}

func phase2ServicePorts(t *testing.T, repo Repository) phase2ContextServicePorts {
	t.Helper()
	api, ok := any(NewService(repo)).(phase2ContextServicePorts)
	if !ok {
		t.Fatal("the real memory.NewService facade lacks SourceAuthorizer/SourceScopeEditor; public serve wiring cannot expose repository capability")
	}
	return api
}

type phase2ServiceOperation struct {
	Name           string
	Call           func(phase2ContextServicePorts, context.Context, Scope) (any, error)
	ExpectedCall   phase2ServiceCall
	ExpectedResult any
	EmptyResult    any
}

func phase2ServiceOperations() (*phase2ContextServiceRepo, []phase2ServiceOperation) {
	ref := Ref{ID: NewID(), Kind: SourceKind, Version: 7}
	studio := NewID()
	recipient := Recipient{PrincipalID: "synthetic-secretary", Role: "secretary", Model: "synthetic-model", Provider: "local-synthetic", Protocol: "openai", Channel: "api", RouteFingerprint: strings.Repeat("a", 64)}
	authorization := SourceAuthorization{ID: NewID(), SourceID: ref.ID, Recipient: recipient, Purpose: KnowledgePurpose, Scope: HardScope{Kind: StudioContextScope, StudioID: studio}, Revision: 42, Revoked: true, ExplicitDeny: true, CreatedAt: time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)}
	authRequest := SourceAuthorizationRequest{RequestID: "synthetic-auth-request", PolicyID: authorization.ID, Source: ref, ExpectedPolicyRevision: 41, Recipient: recipient, Purpose: authorization.Purpose, Scope: authorization.Scope, Revoke: true}
	scopeRequest := SourceScopeRequest{RequestID: "synthetic-scope-request", Source: ref, ExpectedRevision: 19, Assignments: []SourceScopeAssignment{{Kind: StudioSourceScope, StudioID: studio}, {Kind: GlobalConstraintSourceScope}}}
	repo := &phase2ContextServiceRepo{List: []SourceAuthorization{authorization}, Authorization: SourceAuthorizationResult{Authorization: authorization, Duplicate: true, ActionID: string(NewID()), Undoable: true}, Assignment: SourceScopeResult{Source: ref, Revision: 20, Assignments: scopeRequest.Assignments, Duplicate: true, ActionID: string(NewID()), Undoable: true}}
	return repo, []phase2ServiceOperation{
		{Name: "list_authorizations", Call: func(api phase2ContextServicePorts, ctx context.Context, scope Scope) (any, error) {
			return api.SourceAuthorizations(ctx, scope, ref.ID)
		}, ExpectedCall: phase2ServiceCall{Method: "list_authorizations", ID: ref.ID}, ExpectedResult: repo.List, EmptyResult: []SourceAuthorization(nil)},
		{Name: "set_authorization", Call: func(api phase2ContextServicePorts, ctx context.Context, scope Scope) (any, error) {
			return api.SetSourceAuthorization(ctx, scope, authRequest)
		}, ExpectedCall: phase2ServiceCall{Method: "set_authorization", Authorization: &authRequest}, ExpectedResult: repo.Authorization, EmptyResult: SourceAuthorizationResult{}},
		{Name: "get_scope", Call: func(api phase2ContextServicePorts, ctx context.Context, scope Scope) (any, error) {
			return api.SourceScope(ctx, scope, ref.ID)
		}, ExpectedCall: phase2ServiceCall{Method: "get_scope", ID: ref.ID}, ExpectedResult: repo.Assignment, EmptyResult: SourceScopeResult{}},
		{Name: "set_scope", Call: func(api phase2ContextServicePorts, ctx context.Context, scope Scope) (any, error) {
			return api.SetSourceScope(ctx, scope, scopeRequest)
		}, ExpectedCall: phase2ServiceCall{Method: "set_scope", Assignment: &scopeRequest}, ExpectedResult: repo.Assignment, EmptyResult: SourceScopeResult{}},
	}
}

func TestPhase2ContextServiceForwardsOptionalPoliciesExactly(t *testing.T) {
	for _, mode := range []string{"success", "repository_error"} {
		for _, name := range []string{"list_authorizations", "set_authorization", "get_scope", "set_scope"} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				repo, operations := phase2ServiceOperations()
				if mode == "repository_error" {
					repo.Err = errors.New("synthetic repository controlled failure")
				}
				api := phase2ServicePorts(t, repo)
				scope := Scope{OwnerID: NewID(), PrincipalID: "owner", IsOwner: true}
				type contextKey struct{}
				ctx := context.WithValue(context.Background(), contextKey{}, "synthetic-request-context")
				for _, op := range operations {
					if op.Name != name {
						continue
					}
					result, err := op.Call(api, ctx, scope)
					if err != repo.Err || !reflect.DeepEqual(result, op.ExpectedResult) {
						t.Fatalf("facade changed exact repository result/error: got=%+v %v want=%+v %v", result, err, op.ExpectedResult, repo.Err)
					}
					expected := op.ExpectedCall
					expected.Context, expected.Scope = ctx, scope
					if len(repo.Calls) != 1 || !reflect.DeepEqual(repo.Calls[0], expected) || repo.called {
						t.Fatalf("facade changed context/owner/typed request or made unrelated source calls: %+v want=%+v source_called=%t", repo.Calls, expected, repo.called)
					}
				}
			})
		}
	}
}

func TestPhase2ContextServiceOwnerBoundaryPrecedesRepository(t *testing.T) {
	for _, boundary := range []string{"nonowner", "invalid_owner"} {
		for _, name := range []string{"list_authorizations", "set_authorization", "get_scope", "set_scope"} {
			t.Run(boundary+"/"+name, func(t *testing.T) {
				repo, operations := phase2ServiceOperations()
				api := phase2ServicePorts(t, repo)
				scope := Scope{OwnerID: NewID(), PrincipalID: "owner", IsOwner: true}
				if boundary == "nonowner" {
					scope.IsOwner, scope.PrincipalID = false, "synthetic-reader"
				} else {
					scope.OwnerID = ""
				}
				for _, op := range operations {
					if op.Name == name {
						result, err := op.Call(api, context.Background(), scope)
						if err != ErrForbidden || !reflect.DeepEqual(result, op.EmptyResult) || len(repo.Calls) != 0 || repo.called {
							t.Fatalf("owner boundary did not reject exactly before repository: result=%+v err=%v calls=%+v source_called=%t", result, err, repo.Calls, repo.called)
						}
					}
				}
			})
		}
	}
}

func TestPhase2ContextServiceUnsupportedOptionalPoliciesAreUnavailable(t *testing.T) {
	_, operations := phase2ServiceOperations()
	for _, op := range operations {
		t.Run(op.Name, func(t *testing.T) {
			repo := &recordingRepo{}
			api := phase2ServicePorts(t, repo)
			scope := Scope{OwnerID: NewID(), PrincipalID: "owner", IsOwner: true}
			result, err := op.Call(api, context.Background(), scope)
			if err != ErrUnavailable || !reflect.DeepEqual(result, op.EmptyResult) || repo.called {
				t.Fatalf("Sources-only repository must report exact unavailable without raw source call: result=%+v err=%v source_called=%t", result, err, repo.called)
			}
		})
	}
}
