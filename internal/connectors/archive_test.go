package connectors

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestChatBranchesRolesAndGaps(t *testing.T) {
	b, err := DecodeArchive("conversations.json", []byte(`[{"id":"c1","title":"计划","current_node":"u2","mapping":{"u1":{"parent":null,"message":{"id":"u1","author":{"role":"user"},"create_time":1,"content":{"parts":["我只是考虑去书店"]}}},"a1":{"parent":"u1","message":{"id":"a1","author":{"role":"assistant"},"create_time":2,"content":{"parts":["我建议现在购买"]}}},"u2":{"parent":"u1","message":{"id":"u2","author":{"role":"user"},"create_time":3,"content":{"parts":[{"asset_pointer":"missing-image"}]}}}}}]`))
	if err != nil || len(b.Records) != 3 {
		t.Fatal(b, err)
	}
	var ai, image bool
	for _, r := range b.Records {
		if r.Role == "assistant" {
			ai = true
			if r.Branch != "historical" {
				t.Fatal("branch lost")
			}
		}
		if len(r.MissingAttachments) > 0 {
			image = true
		}
	}
	if !ai || !image {
		t.Fatal("role or attachment gap lost")
	}
	again, err := DecodeArchive("conversations.json", []byte(`{"records":[{"id":"one","text":"原文"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := Normalize(again)
	if err != nil || normalized.Records[0].Version != again.Records[0].Version {
		t.Fatal("unstable version")
	}
}
func TestArchiveLimitsAndUnknownFiles(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	a, _ := z.Create("notes.md")
	_, _ = a.Write([]byte("这是记忆原文"))
	a, _ = z.Create("unsupported.json")
	_, _ = a.Write([]byte(`{"secret":"value"}`))
	_ = z.Close()
	b, err := DecodeArchive("export.zip", buf.Bytes())
	if err != nil || len(b.Records) != 1 || len(b.Gaps) != 1 {
		t.Fatal(b, err)
	}
	if _, err := Normalize(Batch{Records: []Record{{ID: "1", Text: "hello\x00world"}}}); err == nil {
		t.Fatal("NUL accepted")
	}
	if _, err := DecodeArchive("note.txt", []byte(strings.Repeat("x", 1<<20+1))); err == nil {
		t.Fatal("oversized record accepted")
	}
}
