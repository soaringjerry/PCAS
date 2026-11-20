package secretary

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestOnlyThreeSafeWebLinksAreShown(t *testing.T) {
	links := Links([]string{"javascript:alert(1)", "https://user:pw@example.com", "https://a.example/x", "http://b.example", "https://c.example/" + strings.Repeat("x", 500), "https://d.example", "https://e.example"})
	if len(links) != 3 || links[0].Host != "a.example" || links[2].Host != "d.example" {
		t.Fatalf("%+v", links)
	}
}

func TestAnAttachmentIsReadOrTheOwnerIsToldWhyNot(t *testing.T) {
	if input, warning := AttachmentInput("票据", "image/png", "ocr", "文字", nil); input != "" || !strings.HasPrefix(warning, "图片存好了") {
		t.Fatal("an image without vision was given to the model", input)
	}
	if _, warning := AttachmentInput("说明", "application/pdf", "", "", errors.New("x")); !strings.HasPrefix(warning, "文件存好了") {
		t.Fatal(warning)
	}
	input, warning := AttachmentInput("说明", "application/pdf", "text", strings.Repeat("字", attachmentRunes+5), nil)
	if warning != "" || !strings.HasPrefix(input, "附件 说明（text）：\n") || len([]rune(input)) != len([]rune("附件 说明（text）：\n"))+attachmentRunes {
		t.Fatal(len([]rune(input)))
	}
	if AttachmentReceipt("audio/ogg") != "存了一段音频" || AttachmentReceipt("text/plain") != "存了一份文件" {
		t.Fatal("receipt")
	}
}

// cardStore is an in-memory CardUnit.
type cardStore struct{ items map[string]workspace.Item }

func (s cardStore) Timeline(context.Context, []memory.Ref) (map[string]TimelineMetadata, error) {
	return map[string]TimelineMetadata{}, nil
}
func (s cardStore) Item(_ context.Context, id string) (workspace.Item, error) {
	return s.items[id], nil
}

func TestTheCardsShowTheUsedSourcesAndTheShownTasksAsTheyAreNow(t *testing.T) {
	s := cardStore{items: map[string]workspace.Item{"t": {ID: "t", Kind: "task", Status: "doing", Due: "2026-03-01", ProjectID: "p"}}}
	aliases := map[string]workspace.Item{"T1": {ID: "t", Kind: "task", Title: "订车"}, "P1": {ID: "p", Kind: "project", Title: "搬家"}, "I1": {ID: "i", Kind: "idea"}}
	sent := map[string]workspace.Memory{"M1": {ID: "m", Version: 2, Text: "下周搬家"}}
	cards, err := Cards(context.Background(), s, Output{Used: []string{"M1", "M1", "M9"}, Show: []string{"T1", "I1", "T1"}}, sent, nil, aliases, false)
	if err != nil || len(cards) != 2 || cards[0].Kind != "sources" || cards[1].Kind != "tasks" {
		t.Fatalf("%+v %v", cards, err)
	}
	tasks := cards[1].Items.([]workspace.DeskTaskItem)
	if len(tasks) != 1 || tasks[0].Status != "doing" || *tasks[0].Project != "搬家" || tasks[0].Title != "订车" {
		t.Fatalf("%+v", tasks)
	}
}
