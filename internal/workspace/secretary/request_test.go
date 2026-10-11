package secretary

import (
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const (
	requestID = "11111111-1111-4111-8111-111111111111"
	sourceID  = "22222222-2222-4222-8222-222222222222"
	checkID   = "33333333-3333-4333-8333-33333333AAAA"
)

func TestATurnNeedsAnIdentityAndASentenceOrAnAttachment(t *testing.T) {
	attachment := memory.Ref{ID: sourceID, Version: 1, Kind: memory.SourceKind}
	if _, err := CheckRequest(workspace.DeskTurnRequest{RequestID: requestID, Text: "你好"}); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckRequest(workspace.DeskTurnRequest{RequestID: requestID, Attachments: []memory.Ref{attachment}}); err != nil {
		t.Fatal("an attachment without a sentence is a turn", err)
	}
	bad := "not-an-identity"
	for name, req := range map[string]workspace.DeskTurnRequest{
		"no identity":                     {Text: "你好"},
		"nothing said":                    {RequestID: requestID, Text: "   "},
		"a sentence that is too long":     {RequestID: requestID, Text: strings.Repeat("云", 4001)},
		"the same attachment twice":       {RequestID: requestID, Attachments: []memory.Ref{attachment, attachment}},
		"an attachment that is no source": {RequestID: requestID, Attachments: []memory.Ref{{ID: sourceID, Version: 1, Kind: memory.ClaimKind}}},
		"five attachments":                {RequestID: requestID, Text: "x", Attachments: make([]memory.Ref, 5)},
		"an item that is no identity":     {RequestID: requestID, Text: "你好", ThingID: &bad},
	} {
		if _, err := CheckRequest(req); err != memory.ErrInvalid {
			t.Fatal(name, err)
		}
	}
}

func TestACheckOnlyTurnHasItsGroupAsItsConversationAndNoAttachment(t *testing.T) {
	got, err := CheckRequest(workspace.DeskTurnRequest{RequestID: requestID, Text: "检查", SmokeID: checkID})
	if err != nil || got.SmokeID != strings.ToLower(checkID) || got.ConversationID == nil || *got.ConversationID != got.SmokeID {
		t.Fatalf("%+v %v", got, err)
	}
	other := requestID
	if _, err := CheckRequest(workspace.DeskTurnRequest{RequestID: requestID, Text: "检查", SmokeID: checkID, ConversationID: &other}); err != memory.ErrInvalid {
		t.Fatal("a check-only turn stays in its own group", err)
	}
	if _, err := CheckRequest(workspace.DeskTurnRequest{RequestID: requestID, SmokeID: checkID, Attachments: []memory.Ref{{ID: sourceID, Version: 1, Kind: memory.SourceKind}}}); err != memory.ErrInvalid {
		t.Fatal("a check-only turn has no attachment", err)
	}
}
