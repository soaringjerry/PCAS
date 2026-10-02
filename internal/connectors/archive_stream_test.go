package connectors

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func syntheticConversation(id string, times ...int) string {
	mapping := map[string]any{}
	for i, at := range times {
		key := fmt.Sprintf("m%d", i)
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		parent := ""
		if i > 0 {
			parent = fmt.Sprintf("m%d", i-1)
		}
		mapping[key] = map[string]any{"parent": parent, "message": map[string]any{"id": key, "author": map[string]string{"role": role}, "create_time": at, "content": map[string]any{"parts": []string{"消息" + key}}}}
	}
	raw, _ := json.Marshal(map[string]any{"id": id, "title": id, "mapping": mapping, "current_node": fmt.Sprintf("m%d", len(times)-1)})
	return string(raw)
}

func TestStreamArchiveSelectsNewestAndCountsWholeInput(t *testing.T) {
	previous := MaxArchiveRecords
	MaxArchiveRecords = 3
	t.Cleanup(func() { MaxArchiveRecords = previous })
	input := "[" + syntheticConversation("old", 1, 2, 3) + "," + syntheticConversation("new", 100, 101, 102) + "]"
	a, err := OpenArchive(context.Background(), "conversations.json", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Preview.Conversations != 2 || a.Preview.Messages != 6 || a.Preview.FromUser != 4 || a.Preview.LeftOut != 3 {
		t.Fatalf("preview: %+v", a.Preview)
	}
	if a.Preview.Earliest.Unix() != 1 || a.Preview.Latest.Unix() != 102 {
		t.Fatal("time range")
	}
	selected, err := a.Records(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 3 || selected[0].ExpressedAt.Unix() != 102 || selected[2].ExpressedAt.Unix() != 100 {
		t.Fatalf("selection: %+v", selected)
	}
	walked := 0
	if err = a.Walk(context.Background(), func(r Record) error {
		walked++
		if r.Version == "" {
			t.Fatal("missing normalized version")
		}
		return nil
	}); err != nil || walked != 6 {
		t.Fatal("walk", walked, err)
	}
	b, err := DecodeArchive("conversations.json", []byte(input))
	if err != nil || len(b.Records) != 3 {
		t.Fatal(b, err)
	}
	for _, r := range b.Records {
		found := false
		for _, v := range selected {
			if v.ID == r.ID && v.Version == r.Version {
				found = true
			}
		}
		if !found {
			t.Fatal("unstable identity")
		}
	}
}
func TestStreamArchiveConversationOrder(t *testing.T) {
	input := "{\"conversations\":[" + syntheticConversation("old", 50) + "," + syntheticConversation("new", 1, 100) + "],\"exported_at\":\"ignored\"}"
	a, err := OpenArchive(context.Background(), "export.json", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	records, err := a.Records(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if records[0].ConversationID != "new" || records[1].ConversationID != "new" || records[2].ConversationID != "old" {
		t.Fatal("not newest conversation first", records)
	}
}
func TestStreamArchiveErrorsAndSkippedAssets(t *testing.T) {
	zipped := func(files map[string]string) []byte {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for name, text := range files {
			f, _ := z.Create(name)
			_, _ = f.Write([]byte(text))
		}
		_ = z.Close()
		return buf.Bytes()
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		data []byte
		code string
	}{
		{"fake.zip", []byte("not a zip"), "invalid_zip"},
		{"empty.zip", zipped(map[string]string{"image.png": "image"}), "no_supported_records"},
		{"truncated.json", []byte("[" + syntheticConversation("c", 1)), "invalid_json"},
		{"other.bin", []byte("text"), "unsupported_archive"},
	} {
		a, err := OpenArchive(ctx, tc.name, bytes.NewReader(tc.data))
		if a != nil {
			a.Close()
		}
		var failure *ArchiveError
		if !errors.As(err, &failure) || failure.Code != tc.code || failure.Message() == "" {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	data := zipped(map[string]string{"conversations.json": "[" + syntheticConversation("c", 1) + "]", "image.png": "image", "audio.wav": "audio"})
	a, err := OpenArchive(ctx, "export.zip", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Preview.Gaps) != 2 || a.Preview.Messages != 1 {
		t.Fatal(a.Preview)
	}
	a.Close()
	oldUpload, oldArchive := MaxUploadBytes, MaxArchiveBytes
	t.Cleanup(func() { MaxUploadBytes = oldUpload; MaxArchiveBytes = oldArchive })
	MaxUploadBytes = 2
	_, err = OpenArchive(ctx, "one.txt", strings.NewReader("abc"))
	var failure *ArchiveError
	if !errors.As(err, &failure) || failure.Code != "archive_too_large" {
		t.Fatal(err)
	}
	MaxUploadBytes = oldUpload
	MaxArchiveBytes = 10
	_, err = OpenArchive(ctx, "export.zip", bytes.NewReader(data))
	if !errors.As(err, &failure) || failure.Code != "archive_too_large" {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = OpenArchive(canceled, "export.zip", bytes.NewReader(data)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
