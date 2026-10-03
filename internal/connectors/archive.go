package connectors

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func decodeLegacyDocument(name string, data []byte) (Batch, error) {
	out := Batch{Records: []Record{}, Gaps: []string{}}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".txt" || ext == ".md" {
		out.Records = append(out.Records, Record{ID: name, Title: filepath.Base(name), Text: string(data), MediaType: "text/plain"})
		return finishBatch(out)
	}
	var value any
	if ext == ".jsonl" || ext == ".ndjson" {
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var r Record
			if json.Unmarshal(line, &r) != nil {
				return out, fmt.Errorf("invalid_jsonl")
			}
			out.Records = append(out.Records, r)
		}
		return finishBatch(out)
	}
	if json.Unmarshal(data, &value) != nil {
		return out, fmt.Errorf("invalid_json")
	}
	if m, ok := value.(map[string]any); ok {
		if records, ok := m["records"]; ok {
			raw, _ := json.Marshal(records)
			if json.Unmarshal(raw, &out.Records) != nil {
				return out, fmt.Errorf("invalid_records")
			}
			return finishBatch(out)
		}
		if v, ok := m["conversations"]; ok {
			value = v
		} else {
			value = []any{m}
		}
	}
	list, ok := value.([]any)
	if !ok {
		return out, fmt.Errorf("unsupported_archive")
	}
	for _, entry := range list {
		c, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		conversationID := str(c["id"])
		if conversationID == "" {
			conversationID = str(c["uuid"])
		}
		title := str(c["title"])
		if title == "" {
			title = str(c["name"])
		}
		if mapping, ok := c["mapping"].(map[string]any); ok {
			if conversationID == "" {
				return out, fmt.Errorf("conversation_id_required")
			}
			active := map[string]bool{}
			node := str(c["current_node"])
			for steps := 0; node != "" && steps < len(mapping); steps++ {
				if active[node] {
					break
				}
				active[node] = true
				m, _ := mapping[node].(map[string]any)
				node = str(m["parent"])
			}
			keys := make([]string, 0, len(mapping))
			for key := range mapping {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				n, _ := mapping[key].(map[string]any)
				message, _ := n["message"].(map[string]any)
				if message == nil {
					continue
				}
				author, _ := message["author"].(map[string]any)
				role := str(author["role"])
				content, _ := message["content"].(map[string]any)
				parts, _ := content["parts"].([]any)
				texts := []string{}
				gaps := []string{}
				for _, part := range parts {
					if text, ok := part.(string); ok {
						texts = append(texts, text)
					} else {
						gaps = appendGap(gaps, "该消息含图片或其他非文本内容，需从原始归档核对")
					}
				}
				id := str(message["id"])
				if id == "" {
					id = key
				}
				text := strings.Join(texts, "\n")
				if strings.TrimSpace(text) == "" {
					if len(gaps) == 0 {
						continue
					}
					text = "[此消息仅含非文本内容，原件或解析文本尚未接入]"
				}
				branch := "historical"
				if active[key] {
					branch = "active"
				}
				out.Records = append(out.Records, Record{ID: "chatgpt/" + conversationID + "/" + id, Title: title, Text: text, Role: role, ConversationID: conversationID, ParentID: str(n["parent"]), Branch: branch, ExpressedAt: parseTime(message["create_time"]), MissingAttachments: gaps})
			}
		} else if messages, ok := c["chat_messages"].([]any); ok {
			if conversationID == "" {
				return out, fmt.Errorf("conversation_id_required")
			}
			parent := ""
			for _, v := range messages {
				message, _ := v.(map[string]any)
				id := str(message["uuid"])
				if id == "" {
					return out, fmt.Errorf("message_id_required")
				}
				role := str(message["sender"])
				if role == "human" {
					role = "user"
				}
				text := str(message["text"])
				gaps := []string{}
				if attachments, ok := message["attachments"].([]any); ok && len(attachments) > 0 {
					gaps = append(gaps, "消息附件尚未单独解析，请核对原始归档")
				}
				if text == "" {
					text = "[非文本消息；内容尚未解析]"
					gaps = appendGap(gaps, "缺少消息文本")
				}
				out.Records = append(out.Records, Record{ID: "claude/" + conversationID + "/" + id, Title: title, Text: text, Role: role, ConversationID: conversationID, ParentID: parent, Branch: "active", ExpressedAt: parseTime(message["created_at"]), MissingAttachments: gaps})
				parent = id
			}
		} else {
			raw, _ := json.Marshal(c)
			var r Record
			if json.Unmarshal(raw, &r) != nil || r.ID == "" || r.Text == "" {
				return out, fmt.Errorf("unsupported_record")
			}
			out.Records = append(out.Records, r)
		}
	}
	return finishBatch(out)
}
func finishBatch(b Batch) (Batch, error) {
	if len(b.Records) == 0 {
		return b, fmt.Errorf("invalid_record_count")
	}
	if len(b.Gaps) > 100 || len(b.NextCursor) > 4096 {
		return b, fmt.Errorf("invalid_metadata")
	}
	for _, gap := range b.Gaps {
		if len(gap) > 4096 || strings.ContainsRune(gap, 0) || !utf8.ValidString(gap) {
			return b, fmt.Errorf("invalid_metadata")
		}
	}
	for i := range b.Records {
		r := &b.Records[i]
		if r.ID == "" || len(r.ID) > 1024 || strings.TrimSpace(r.Text) == "" || len(r.Text) > 1<<20 || len(r.Title) > 4096 {
			return b, fmt.Errorf("invalid_record")
		}
		for _, v := range []string{r.ID, r.Version, r.Title, r.Text, r.ConversationID, r.ParentID, r.Role, r.Branch, r.EpisodeKey, r.EpisodeTitle} {
			if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
				return b, fmt.Errorf("invalid_text")
			}
		}
		if len(r.Version) > 256 || len(r.ConversationID) > 1024 || len(r.ParentID) > 1024 || len(r.Role) > 40 || len(r.Branch) > 100 || len(r.EpisodeKey) > 1024 || len(r.EpisodeTitle) > 4096 || len(r.MissingAttachments) > 100 {
			return b, fmt.Errorf("invalid_metadata")
		}
		for _, gap := range r.MissingAttachments {
			if len(gap) > 4096 || strings.ContainsRune(gap, 0) || !utf8.ValidString(gap) {
				return b, fmt.Errorf("invalid_metadata")
			}
		}
		if r.MediaType == "" {
			r.MediaType = "text/plain"
		}
		if r.MediaType != "text/plain" && r.MediaType != "text/markdown" {
			return b, fmt.Errorf("unsupported_media")
		}
		if r.Title == "" {
			r.Title = "导入的记录"
		}
		if r.Version == "" {
			raw, _ := json.Marshal(r)
			r.Version = fmt.Sprintf("%x", sha256.Sum256(raw))
		}
		if r.EpisodeTitle == "" {
			r.EpisodeTitle = r.Title
		}
	}
	sort.SliceStable(b.Records, func(i, j int) bool {
		a, z := b.Records[i], b.Records[j]
		if (a.ExpressedAt == nil) != (z.ExpressedAt == nil) {
			return a.ExpressedAt != nil
		}
		if a.ExpressedAt != nil && !a.ExpressedAt.Equal(*z.ExpressedAt) {
			return a.ExpressedAt.Before(*z.ExpressedAt)
		}
		return a.ID < z.ID
	})
	return b, nil
}
func Normalize(b Batch) (Batch, error) { return finishBatch(b) }
func str(v any) string                 { s, _ := v.(string); return s }
func parseTime(v any) *time.Time {
	switch x := v.(type) {
	case float64:
		t := time.Unix(int64(x), int64((x-float64(int64(x)))*1e9)).UTC()
		return &t
	case string:
		t, e := time.Parse(time.RFC3339Nano, x)
		if e == nil {
			return &t
		}
	}
	return nil
}
func appendGap(gaps []string, gap string) []string {
	if len(gaps) < 100 {
		return append(gaps, gap)
	}
	return gaps
}
