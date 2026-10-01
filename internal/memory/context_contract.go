package memory

import (
	"context"
	"encoding/hex"
	"strings"
	"time"
)

// These additive contracts describe phase-two consumers. Their presence does
// not enable source access or replace PostgreSQL permission/version checks.
type ContextPurpose string

const (
	KnowledgePurpose         ContextPurpose = "knowledge"
	ExecutionEvidencePurpose ContextPurpose = "execution_evidence"
	RawAuditPurpose          ContextPurpose = "raw_audit"
)

type IngestPurpose string

const (
	HistoricalMemoryImport IngestPurpose = "historical_memory_import"
	CurrentInstruction     IngestPurpose = "current_instruction"
	IncomingMaterial       IngestPurpose = "incoming_material"
)

type ContextScopeKind string

const (
	StudioContextScope      ContextScopeKind = "studio"
	OwnerGlobalContextScope ContextScopeKind = "owner_global"
	UnscopedContextScope    ContextScopeKind = "unscoped"
)

// HardScope is an authorization boundary. WorkingContext.Objects remains a
// relevance hint and cannot widen this scope.
type HardScope struct {
	Kind                     ContextScopeKind `json:"kind"`
	StudioID                 ID               `json:"studio_id,omitempty"`
	IncludeGlobalConstraints bool             `json:"include_global_constraints"`
}

func (s HardScope) Valid() bool {
	switch s.Kind {
	case StudioContextScope:
		return s.StudioID.Valid()
	case OwnerGlobalContextScope, UnscopedContextScope:
		return s.StudioID == "" && !s.IncludeGlobalConstraints
	default:
		return false
	}
}

// Recipient identifies the actual destination, not a user-editable model name.
// PrincipalID uses the existing workspace agent ID unchanged. RouteFingerprint
// is SHA-256 of the normalized server route identity, never a credential hash.
type Recipient struct {
	PrincipalID      string `json:"principal_id"`
	Role             string `json:"role"`
	Model            string `json:"model"`
	Provider         string `json:"provider"`
	Protocol         string `json:"protocol"`
	Channel          string `json:"channel"`
	RouteFingerprint string `json:"route_fingerprint"`
}

func (r Recipient) Valid() bool {
	if r.PrincipalID == "" || r.Role == "" || r.Model == "" || r.Provider == "" || r.Protocol == "" || r.Channel == "" {
		return false
	}
	fingerprint, err := hex.DecodeString(r.RouteFingerprint)
	return err == nil && len(fingerprint) == 32 && r.RouteFingerprint == strings.ToLower(r.RouteFingerprint)
}

type VersionView struct {
	Mode    RecallMode `json:"mode"` // continue/remember=current; history=explicit history
	ValidAt *time.Time `json:"valid_at,omitempty"`
	KnownAt *time.Time `json:"known_at,omitempty"`
}

// TrustedTaskContext is built from authentication, the current owner intent,
// server task records and the final provider route. Never decode it from JSON.
type TrustedTaskContext struct {
	OwnerID          ID
	Recipient        Recipient
	Purpose          ContextPurpose
	Scope            HardScope
	View             VersionView
	Now              time.Time
	Timezone         string
	MemoryBudget     Budget
	TotalInputTokens int
}

type SourceScopeKind string

const (
	StudioSourceScope           SourceScopeKind = "studio"
	GlobalConstraintSourceScope SourceScopeKind = "global_constraint"
	UnscopedSourceScope         SourceScopeKind = "unscoped"
)

type SourceScopeAssignment struct {
	Kind     SourceScopeKind `json:"kind"`
	StudioID ID              `json:"studio_id,omitempty"`
}

type SourceScopeResult struct {
	Source      Ref                     `json:"source"`
	Revision    int                     `json:"revision"`
	Assignments []SourceScopeAssignment `json:"assignments"`
	Duplicate   bool                    `json:"duplicate"`
}

type SourceScopeRequest struct {
	RequestID        string                  `json:"request_id"`
	Source           Ref                     `json:"source"`
	ExpectedRevision int                     `json:"expected_revision"`
	Assignments      []SourceScopeAssignment `json:"assignments"`
}

type SourceScopeEditor interface {
	SourceScope(context.Context, Scope, ID) (SourceScopeResult, error)
	SetSourceScope(context.Context, Scope, SourceScopeRequest) (SourceScopeResult, error)
}

// SourceAuthorization binds a stable source identity to one exact recipient,
// purpose and scope. Revoked is an explicit deny for that source and any of its
// evidence-derived content; absence never denies independently granted claims.
// Revision increases on grant, revoke, narrow and undo.
type SourceAuthorization struct {
	ID        ID             `json:"id"`
	SourceID  ID             `json:"source_id"`
	Recipient Recipient      `json:"recipient"`
	Purpose   ContextPurpose `json:"purpose"`
	Scope     HardScope      `json:"scope"`
	Revision  int            `json:"revision"`
	Revoked   bool           `json:"revoked"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type SourceAuthorizationRequest struct {
	RequestID string `json:"request_id"`
	// PolicyID identifies an existing policy for atomic tuple replacement or
	// narrowing. New policies use an empty ID and expected revision zero.
	PolicyID               ID             `json:"policy_id,omitempty"`
	Source                 Ref            `json:"source"` // exact expected current source version
	ExpectedPolicyRevision int            `json:"expected_policy_revision"`
	Recipient              Recipient      `json:"recipient"`
	Purpose                ContextPurpose `json:"purpose"`
	Scope                  HardScope      `json:"scope"`
	Revoke                 bool           `json:"revoke"`
}

type SourceAuthorizationResult struct {
	Authorization SourceAuthorization `json:"authorization"`
	Duplicate     bool                `json:"duplicate"`
}

// Optional ports keep legacy Sources/API implementations and stubs compatible.
type SourceAuthorizer interface {
	SourceAuthorizations(context.Context, Scope, ID) ([]SourceAuthorization, error)
	SetSourceAuthorization(context.Context, Scope, SourceAuthorizationRequest) (SourceAuthorizationResult, error)
}

// AuthorizationStamp fences results against revoke/regrant and policy undo.
// ScopeRevision similarly prevents a source membership change from reviving
// a previously prepared context. Neither stamp substitutes for live checks.
type AuthorizationStamp struct {
	PolicyID ID  `json:"policy_id"`
	Revision int `json:"revision"`
}

type TypedDependency struct {
	Ref           Ref                 `json:"ref"`
	Purpose       ContextPurpose      `json:"purpose"`
	Scope         HardScope           `json:"scope"`
	Authorization *AuthorizationStamp `json:"authorization,omitempty"`
	ScopeRevision int                 `json:"scope_revision"`
}

// SourceSpan is a rune offset in the exact source version, [StartRune, EndRune).
type SourceSpan struct {
	Source    Ref `json:"source"`
	StartRune int `json:"start_rune"`
	EndRune   int `json:"end_rune"`
}

func (s SourceSpan) Valid() bool {
	return s.Source.ID.Valid() && s.Source.Kind == SourceKind && s.Source.Version > 0 && s.StartRune >= 0 && s.EndRune > s.StartRune
}

// EvidenceEntry is transient hydrated text. Persist ContextManifest rather than
// this value in diagnostic metadata, to avoid a second copy of the body.
type EvidenceEntry struct {
	Ref          Ref               `json:"ref"`
	SourceSpan   *SourceSpan       `json:"source_span,omitempty"`
	Text         string            `json:"text"`
	Role         string            `json:"role,omitempty"`
	ExpressedAt  *time.Time        `json:"expressed_at,omitempty"`
	Historical   bool              `json:"historical"`
	Changed      bool              `json:"changed"`
	Dependencies []TypedDependency `json:"dependencies"`
	Gaps         []string          `json:"gaps"`
}

// ContextKindSupported means that phase two promises a verifier for this kind;
// it does not mean the verifier has run. ChunkKind must first normalize to a
// checked SourceSpan. All other kinds fail closed until separately implemented.
func ContextKindSupported(k Kind) bool {
	return k == SourceKind || k == ClaimKind || k == SummaryKind
}

type CandidateRecord struct {
	Ref         Ref         `json:"ref"`
	SourceSpan  *SourceSpan `json:"source_span,omitempty"`
	Stage       string      `json:"stage"`
	Disposition string      `json:"disposition"`
	Reason      string      `json:"reason,omitempty"`
}

// PayloadSpan uses byte offsets in the one retained final payload, not runes.
type PayloadSpan struct {
	StartByte int `json:"start_byte"`
	EndByte   int `json:"end_byte"`
}

type InputRecord struct {
	Ref          Ref           `json:"ref"`
	SourceSpan   *SourceSpan   `json:"source_span,omitempty"`
	PayloadSpans []PayloadSpan `json:"payload_spans"`
	Truncated    bool          `json:"truncated"`
}

type UsedRecord struct {
	Ref        Ref    `json:"ref"`
	Validation string `json:"validation"` // supported, absent_from_input, forbidden, unknown
}

type TokenCount struct {
	Value  *int   `json:"value,omitempty"`
	Method string `json:"method"` // actual, estimated, unknown
	Model  string `json:"model,omitempty"`
}

// ContextManifest contains references and mappings, never hydrated source text.
type ContextManifest struct {
	Version              int               `json:"version"`
	AttemptID            ID                `json:"attempt_id"`
	Recipient            Recipient         `json:"recipient"`
	Purpose              ContextPurpose    `json:"purpose"`
	Scope                HardScope         `json:"scope"`
	View                 VersionView       `json:"view"`
	Candidates           []CandidateRecord `json:"candidates"`
	Input                []InputRecord     `json:"input"`
	Used                 []UsedRecord      `json:"used"`
	IndirectDependencies []TypedDependency `json:"indirect_dependencies"`
	Coverage             Coverage          `json:"coverage"`
	Truncated            bool              `json:"truncated"`
	InputBytes           int               `json:"input_bytes"`
	InputTokens          TokenCount        `json:"input_tokens"`
	ObservationLayer     string            `json:"observation_layer"` // adapter_arguments, serialized_request, manual_package
}

type AttemptState string

const (
	AttemptPrepared       AttemptState = "prepared"
	AttemptDispatched     AttemptState = "dispatched"
	AttemptCompleted      AttemptState = "completed"
	AttemptFailed         AttemptState = "failed"
	AttemptOutcomeUnknown AttemptState = "outcome_unknown"
	AttemptInvalidated    AttemptState = "invalidated"
)

type SnapshotState string

const (
	SnapshotRetained        SnapshotState = "retained"
	SnapshotExpired         SnapshotState = "expired"
	SnapshotDeleted         SnapshotState = "deleted"
	SnapshotRevoked         SnapshotState = "revoked"
	SnapshotCapacityOmitted SnapshotState = "capacity_omitted"
)

type ContextAttempt struct {
	ID                 ID              `json:"id"`
	OperationID        string          `json:"operation_id"`
	Ordinal            int             `json:"ordinal"`
	State              AttemptState    `json:"state"`
	SnapshotState      SnapshotState   `json:"snapshot_state"`
	Manifest           ContextManifest `json:"manifest"`
	CreatedAt          time.Time       `json:"created_at"`
	DispatchReservedAt *time.Time      `json:"dispatch_reserved_at,omitempty"`
	DispatchedAt       *time.Time      `json:"dispatched_at,omitempty"`
	DeliveredAt        *time.Time      `json:"delivered_at,omitempty"` // PCAS manual response; not external receipt
	CompletedAt        *time.Time      `json:"completed_at,omitempty"`
	ExternalReceipt    string          `json:"external_receipt"` // unknown or acknowledged
	InvalidationReason string          `json:"invalidation_reason,omitempty"`
	BodyExpiresAt      time.Time       `json:"body_expires_at"`
	MetadataExpiresAt  time.Time       `json:"metadata_expires_at"`
}

type ContextInvalidationReason string

const (
	ContextCorrected    ContextInvalidationReason = "corrected"
	ContextReplaced     ContextInvalidationReason = "replaced"
	ContextDeleted      ContextInvalidationReason = "deleted"
	ContextRevoked      ContextInvalidationReason = "revoked"
	ContextScopeChanged ContextInvalidationReason = "scope_changed"
)

type ContextInvalidation struct {
	RecordIDs []ID
	Reason    ContextInvalidationReason
	// Non-nil limits policy revocation to this actual recipient. Deletion and
	// source replacement must not use this filter to spare dependent copies.
	Recipient *Recipient
}

// Initial engineering limits; no latency, cost or model-token claim is implied.
const (
	DefaultContextBodyRetention        = 7 * 24 * time.Hour
	DefaultContextMetadataRetention    = 30 * 24 * time.Hour
	DefaultContextOwnerBodyBytes       = 64 << 20
	DefaultContextAttemptBodyBytes     = 256 << 10
	DefaultContextAttemptMetadataBytes = 64 << 10
	DefaultContextOwnerMetadataBytes   = 64 << 20
	DefaultContextOwnerAttempts        = 10000
	DefaultContextCleanupInterval      = time.Hour
)
