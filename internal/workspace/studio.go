package workspace

import (
	"context"
	"errors"
	"io"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// StudioAPI freezes the phase 3 contract. Read methods never mutate data.
type StudioAPI interface {
	ReadProjectHandover(context.Context, memory.Scope, string) (ProjectHandover, error)
	ListDocumentVersions(context.Context, memory.Scope, string) (DocumentVersionList, error)
	ReadDocumentVersion(context.Context, memory.Scope, string, int) (DocumentVersion, error)
	ReadDocumentDiff(context.Context, memory.Scope, string, int, int) (DocumentDiff, error)
	ListProjectFiles(context.Context, memory.Scope, string) (ProjectFileList, error)
	UploadProjectFile(context.Context, memory.Scope, string, string, string, io.Reader) (ProjectFileUpload, error)
	DeleteProjectFile(context.Context, memory.Scope, string, string, bool) error
	ReadProjectTimeline(context.Context, memory.Scope, string) (ProjectTimeline, error)
}

// Kind is memory, item, documentVersion or run. Version identifies the
// referenced memory/document revision; the ID belongs to the same owner.
type ProjectEvidence struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"`
}
type ProjectSentence struct {
	Text     string            `json:"text"`
	Evidence []ProjectEvidence `json:"evidence"`
}
type ProjectHandover struct {
	ProjectID  string            `json:"projectId"`
	Conclusion []ProjectSentence `json:"conclusion"`
	Blockers   []ProjectSentence `json:"blockers"`
	NextSteps  []ProjectSentence `json:"nextSteps"`
	WrittenAt  *string           `json:"writtenAt"`
	Stale      bool              `json:"stale"`
}
type DocumentVersion struct {
	DocumentID string  `json:"documentId"`
	Version    int     `json:"version"`
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	WrittenAt  string  `json:"writtenAt"`
	Author     string  `json:"author"` // user, deputy or secretary
	BasedOn    *int    `json:"basedOn"`
	RunID      *string `json:"runId"`
}
type DocumentVersionSummary struct {
	DocumentID string  `json:"documentId"`
	Version    int     `json:"version"`
	WrittenAt  string  `json:"writtenAt"`
	Author     string  `json:"author"`
	BasedOn    *int    `json:"basedOn"`
	RunID      *string `json:"runId"`
}
type DocumentVersionList struct {
	Items          []DocumentVersionSummary `json:"items"`
	CurrentVersion int                      `json:"currentVersion"`
}

// Changes are ordered paragraph edits; indexes are zero based. A missing side
// has index null and text empty. Kind is add, delete or change.
type ParagraphChange struct {
	Kind        string `json:"kind"`
	BeforeIndex *int   `json:"beforeIndex"`
	AfterIndex  *int   `json:"afterIndex"`
	Before      string `json:"before"`
	After       string `json:"after"`
}
type DocumentDiff struct {
	DocumentID  string            `json:"documentId"`
	FromVersion int               `json:"fromVersion"`
	ToVersion   int               `json:"toVersion"`
	Changes     []ParagraphChange `json:"changes"`
}
type ProjectFile struct {
	SourceID      string  `json:"sourceId"`
	ProjectID     string  `json:"projectId"`
	Name          string  `json:"name"`
	MediaType     string  `json:"mediaType"`
	Size          int64   `json:"size"`
	CreatedAt     string  `json:"createdAt"`
	Status        string  `json:"status"` // stored, extracted or failed
	FailureReason string  `json:"failureReason"`
	JobID         *string `json:"jobId"`
	OpenURL       string  `json:"openUrl"`
}
type ProjectFileList struct {
	Items []ProjectFile `json:"items"`
}
type ProjectFileUpload struct {
	File          ProjectFile `json:"file"`
	AlreadyExists bool        `json:"alreadyExists"`
}
type TimelineItem struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	StartDate      *string  `json:"startDate"`
	Due            string   `json:"due"`
	Status         string   `json:"status"`
	EstimatedHours *float64 `json:"estimatedHours"`
	Overdue        bool     `json:"overdue"`
}
type ProjectTimeline struct {
	ProjectID  string         `json:"projectId"`
	Items      []TimelineItem `json:"items"`
	WithoutDue []Item         `json:"withoutDue"`
	DailyHours float64        `json:"dailyHours"`
}
type RunDocumentVersion struct {
	DocumentID string `json:"documentId"`
	Version    int    `json:"version"`
}

var ErrFileTooLarge = errors.New("file exceeds attachment limit")
var ErrFileEmpty = errors.New("file is empty")
var ErrFileConfirmation = errors.New("file deletion requires confirmation")
