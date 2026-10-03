// Package connectors normalizes independent applications into versioned sources.
package connectors

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/memory"
	"time"
)

type Record struct {
	ID                 string     `json:"id"`
	Version            string     `json:"version"`
	Title              string     `json:"title"`
	Text               string     `json:"text"`
	MediaType          string     `json:"media_type,omitempty"`
	ExpressedAt        *time.Time `json:"expressed_at,omitempty"`
	ConversationID     string     `json:"conversation_id,omitempty"`
	ParentID           string     `json:"parent_id,omitempty"`
	Role               string     `json:"role,omitempty"`
	Branch             string     `json:"branch,omitempty"`
	EpisodeKey         string     `json:"episode_key,omitempty"`
	EpisodeTitle       string     `json:"episode_title,omitempty"`
	MissingAttachments []string   `json:"missing_attachments,omitempty"`
}
type Batch struct {
	Records    []Record `json:"records"`
	NextCursor string   `json:"next_cursor,omitempty"`
	HasMore    bool     `json:"has_more,omitempty"`
	Gaps       []string `json:"gaps,omitempty"`
}
type Connection struct {
	ID              memory.ID  `json:"id"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	URL             string     `json:"url,omitempty"`
	TokenEnv        string     `json:"token_env,omitempty"`
	IntervalSeconds int        `json:"interval_seconds"`
	Enabled         bool       `json:"enabled"`
	Version         int        `json:"version"`
	Status          string     `json:"status"`
	LastSync        *time.Time `json:"last_sync,omitempty"`
	NextSync        time.Time  `json:"next_sync"`
	Imported        int        `json:"imported"`
	Error           string     `json:"error,omitempty"`
	Gaps            []string   `json:"gaps"`
	Folder          string     `json:"folder,omitempty"`
}
type ConfigureRequest struct {
	ID              memory.ID `json:"id,omitempty"`
	ExpectedVersion int       `json:"expected_version"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	URL             string    `json:"url,omitempty"`
	TokenEnv        string    `json:"token_env,omitempty"`
	IntervalSeconds int       `json:"interval_seconds"`
	Enabled         bool      `json:"enabled"`
}
type Configured struct {
	Connection   Connection `json:"connection"`
	WebhookToken string     `json:"webhook_token,omitempty"`
}
type Result struct {
	BatchID    memory.ID    `json:"batchId,omitempty"`
	Refs       []memory.Ref `json:"refs"`
	Imported   int          `json:"imported"`
	Duplicates int          `json:"duplicates"`
	Blocked    int          `json:"blocked"`
	Gaps       []string     `json:"gaps"`
}
type API interface {
	ListConnections(context.Context, memory.Scope) ([]Connection, error)
	ConfigureConnection(context.Context, memory.Scope, ConfigureRequest) (Configured, error)
	SyncConnection(context.Context, memory.Scope, memory.ID, int) error
	ImportBatch(context.Context, memory.Scope, memory.ID, Batch) (Result, error)
	ImportArchive(context.Context, memory.Scope, string, []byte) (Result, error)
	AuthenticateConnection(context.Context, memory.ID, string) (memory.Scope, error)
}

type ImportBatch struct {
	ID             memory.ID  `json:"id"`
	ArchiveID      memory.ID  `json:"archiveId"`
	ArchiveVersion int        `json:"archiveVersion"`
	Name           string     `json:"name"`
	State          string     `json:"state"`
	Total          int        `json:"total"`
	Stored         int        `json:"stored"`
	Organized      int        `json:"organized"`
	OrganizeLater  bool       `json:"organizeLater"`
	LeftOut        int        `json:"leftOut"`
	Earliest       *time.Time `json:"earliest"`
	Latest         *time.Time `json:"latest"`
	ErrorCode      string     `json:"errorCode"`
	Error          string     `json:"error"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}
