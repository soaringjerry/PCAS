package secretary

import (
	"strings"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const (
	// textLimit is the longest sentence of one turn, in characters.
	textLimit = 4000
	// attachmentLimit is the count of attachments of one turn.
	attachmentLimit = 4
)

// CheckRequest checks the request of one turn and returns it in its normal
// form. A turn needs a request identity and a sentence or an attachment.
// Each attachment is a distinct source version. A check-only turn has no
// attachment, and its conversation is its check group.
func CheckRequest(req workspace.DeskTurnRequest) (workspace.DeskTurnRequest, error) {
	if req.SmokeID != "" {
		if !memory.ID(req.SmokeID).Valid() || len(req.Attachments) != 0 || (req.ConversationID != nil && !strings.EqualFold(*req.ConversationID, req.SmokeID)) {
			return req, memory.ErrInvalid
		}
		req.SmokeID = strings.ToLower(req.SmokeID)
		req.ConversationID = &req.SmokeID
	}
	text := strings.TrimSpace(req.Text)
	if !memory.ID(req.RequestID).Valid() || (text == "" && len(req.Attachments) == 0) || len(req.Attachments) > attachmentLimit || utf8.RuneCountInString(text) > textLimit {
		return req, memory.ErrInvalid
	}
	seen := map[memory.ID]bool{}
	for _, ref := range req.Attachments {
		if !ref.ID.Valid() || ref.Version < 1 || ref.Kind != memory.SourceKind || seen[ref.ID] {
			return req, memory.ErrInvalid
		}
		seen[ref.ID] = true
	}
	for _, id := range []*string{req.ConversationID, req.ThingID} {
		if id != nil && !memory.ID(*id).Valid() {
			return req, memory.ErrInvalid
		}
	}
	return req, nil
}
