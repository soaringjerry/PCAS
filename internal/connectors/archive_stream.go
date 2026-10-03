package connectors

import (
	"archive/zip"
	"bytes"
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var MaxUploadBytes int64 = 512 << 20
var MaxArchiveBytes int64 = 512 << 20
var MaxArchiveRecords = 200000

var conversationFileName = regexp.MustCompile(`(?i)^conversations(?:[-_]\d+)?\.json$`)

func isConversationFile(name string) bool {
	return conversationFileName.MatchString(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
}

// ArchiveError carries a fixed code and a user-facing recovery suggestion.
// It never includes source text, paths or parser output.
type ArchiveError struct{ Code string }

func (e *ArchiveError) Error() string { return e.Code }
func (e *ArchiveError) Message() string {
	switch e.Code {
	case "import_not_active":
		return "导入状态已改变，请刷新进度后再操作。"
	case "invalid_zip":
		return "这个文件打不开，请重新下载原始 zip 文件再试。"
	case "unsupported_archive":
		return "暂不认识这种文件，请使用聊天导出的 JSON、zip 或文本文件。"
	case "no_supported_records":
		return "文件里没有可读的对话，请检查是否选择了聊天导出文件。"
	case "invalid_json":
		return "对话文件不完整或格式有误，请重新导出后再试。"
	case "archive_too_large":
		return "文件超过可读取的大小，请拆成几份后再导入。"
	default:
		return "这份记录的格式有误，请检查原始导出文件后重试。"
	}
}
func archiveError(code string) error { return &ArchiveError{Code: code} }

// ArchivePreview describes the entire input; LeftOut accounts for the messages
// outside the newest-record selection. The normalized spool is temporary only.
type ArchivePreview struct {
	Name            string     `json:"name"`
	Conversations   int        `json:"conversations"`
	Messages        int        `json:"messages"`
	FromUser        int        `json:"fromUser"`
	Earliest        *time.Time `json:"earliest"`
	Latest          *time.Time `json:"latest"`
	AlreadyImported int        `json:"alreadyImported"`
	Blocked         int        `json:"blocked"`
	LeftOut         int        `json:"leftOut"`
	Gaps            []string   `json:"gaps"`
}

type archiveEntry struct {
	offset         int64
	size           int
	at             time.Time
	conversationAt time.Time
	id             string
}
type entryHeap []archiveEntry

func (h entryHeap) Len() int           { return len(h) }
func (h entryHeap) Less(i, j int) bool { return olderEntry(h[i], h[j]) }
func (h entryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *entryHeap) Push(v any)        { *h = append(*h, v.(archiveEntry)) }
func (h *entryHeap) Pop() any          { old := *h; v := old[len(old)-1]; *h = old[:len(old)-1]; return v }
func olderEntry(a, b archiveEntry) bool {
	if !a.at.Equal(b.at) {
		return a.at.Before(b.at)
	}
	if a.id != b.id {
		return a.id > b.id
	}
	return a.offset > b.offset
}

type Archive struct {
	Preview  ArchivePreview
	Hash     string
	original *os.File
	spool    *os.File
	entries  entryHeap
	offset   int64
}

func (a *Archive) Close() {
	for _, f := range []*os.File{a.original, a.spool} {
		if f != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}
}
func (a *Archive) Original() io.ReadSeeker {
	_, _ = a.original.Seek(0, io.SeekStart)
	return a.original
}
func (a *Archive) Len() int { return len(a.entries) }
func (a *Archive) Records(start, count int) ([]Record, error) {
	if start < 0 || start > len(a.entries) || count < 0 {
		return nil, archiveError("invalid_record_count")
	}
	end := min(start+count, len(a.entries))
	records := make([]Record, 0, end-start)
	for _, e := range a.entries[start:end] {
		data := make([]byte, e.size)
		if _, err := a.spool.ReadAt(data, e.offset); err != nil {
			return nil, err
		}
		var r Record
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// Walk visits every normalized message, including those excluded by the cap.
// The visitor can batch identity checks without retaining the archive in memory.
func (a *Archive) Walk(ctx context.Context, visit func(Record) error) error {
	d := json.NewDecoder(io.NewSectionReader(a.spool, 0, a.offset))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var r Record
		err := d.Decode(&r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err = visit(r); err != nil {
			return err
		}
	}
}

type archiveReader struct {
	ctx       context.Context
	r         io.Reader
	remaining *int64
}

func (r *archiveReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if *r.remaining == 0 {
		var b [1]byte
		n, err := r.r.Read(b[:])
		if n > 0 {
			return 0, archiveError("archive_too_large")
		}
		return 0, err
	}
	if int64(len(p)) > *r.remaining {
		p = p[:*r.remaining]
	}
	n, err := r.r.Read(p)
	*r.remaining -= int64(n)
	return n, err
}

// OpenArchive spools bytes and normalized records to private temporary files.
// JSON is decoded one conversation at a time; only a bounded heap of offsets
// for the newest messages remains in memory. Text lives in the spool, not heap.
func OpenArchive(ctx context.Context, name string, input io.Reader) (_ *Archive, err error) {
	a := &Archive{Preview: ArchivePreview{Name: name, Gaps: []string{}}}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()
	a.original, err = os.CreateTemp("", "pcas-archive-original-")
	if err != nil {
		return nil, err
	}
	a.spool, err = os.CreateTemp("", "pcas-archive-records-")
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	remaining := MaxUploadBytes
	_, err = io.Copy(io.MultiWriter(a.original, hash), &archiveReader{ctx: ctx, r: input, remaining: &remaining})
	if err != nil {
		return nil, err
	}
	a.Hash = hex.EncodeToString(hash.Sum(nil))
	size, err := a.original.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	remaining = MaxArchiveBytes
	if strings.EqualFold(filepath.Ext(name), ".zip") {
		z, e := zip.NewReader(a.original, size)
		if e != nil {
			return nil, archiveError("invalid_zip")
		}
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(f.Name))
			if !isConversationFile(f.Name) && ext != ".txt" && ext != ".md" && ext != ".jsonl" && ext != ".ndjson" {
				a.Preview.Gaps = appendGap(a.Preview.Gaps, "已跳过归档中的非对话文件："+filepath.Base(f.Name))
				continue
			}
			if f.UncompressedSize64 > uint64(max(remaining, 0)) {
				return nil, archiveError("archive_too_large")
			}
			r, e := f.Open()
			if e != nil {
				return nil, archiveError("invalid_zip")
			}
			e = a.decode(ctx, f.Name, &archiveReader{ctx: ctx, r: r, remaining: &remaining})
			closeErr := r.Close()
			if e != nil {
				return nil, e
			}
			if closeErr != nil {
				return nil, archiveError("invalid_zip")
			}
		}
	} else {
		_, err = a.original.Seek(0, io.SeekStart)
		if err != nil {
			return nil, err
		}
		err = a.decode(ctx, name, &archiveReader{ctx: ctx, r: a.original, remaining: &remaining})
		if err != nil {
			return nil, err
		}
	}
	if a.Preview.Messages == 0 {
		return nil, archiveError("no_supported_records")
	}
	a.Preview.LeftOut = a.Preview.Messages - len(a.entries)
	a.sortEntries()
	return a, nil
}
func (a *Archive) add(batch Batch) error {
	var conversationAt time.Time
	for _, r := range batch.Records {
		if r.ExpressedAt != nil && r.ExpressedAt.After(conversationAt) {
			conversationAt = *r.ExpressedAt
		}
	}
	for _, r := range batch.Records {
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if _, err = a.spool.Write(data); err != nil {
			return err
		}
		e := archiveEntry{offset: a.offset, size: len(data), id: r.ID, conversationAt: conversationAt}
		if r.ExpressedAt != nil {
			e.at = *r.ExpressedAt
		}
		a.offset += int64(len(data))
		a.Preview.Messages++
		if r.Role == "user" {
			a.Preview.FromUser++
		}
		if r.ExpressedAt != nil {
			if a.Preview.Earliest == nil || r.ExpressedAt.Before(*a.Preview.Earliest) {
				v := *r.ExpressedAt
				a.Preview.Earliest = &v
			}
			if a.Preview.Latest == nil || r.ExpressedAt.After(*a.Preview.Latest) {
				v := *r.ExpressedAt
				a.Preview.Latest = &v
			}
		}
		for _, gap := range r.MissingAttachments {
			a.Preview.Gaps = appendGap(a.Preview.Gaps, gap)
		}
		a.keep(e)
	}
	for _, gap := range batch.Gaps {
		a.Preview.Gaps = appendGap(a.Preview.Gaps, gap)
	}
	return nil
}
func jsonArchiveError(err error) error {
	var failure *ArchiveError
	if errors.As(err, &failure) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return archiveError("invalid_json")
}
func (a *Archive) decode(ctx context.Context, name string, r io.Reader) error {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".txt" || ext == ".md" {
		data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
		if err != nil {
			return err
		}
		b, err := decodeLegacyDocument(name, data)
		if err != nil {
			return archiveError(err.Error())
		}
		return a.add(b)
	}
	if ext == ".jsonl" || ext == ".ndjson" {
		d := json.NewDecoder(r)
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			var record Record
			err := d.Decode(&record)
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return jsonArchiveError(err)
			}
			b, err := Normalize(Batch{Records: []Record{record}})
			if err != nil {
				return archiveError(err.Error())
			}
			if err = a.add(b); err != nil {
				return err
			}
		}
	}
	if ext != ".json" {
		return archiveError("unsupported_archive")
	}
	d := json.NewDecoder(r)
	token, err := d.Token()
	if err != nil {
		return jsonArchiveError(err)
	}
	switch token {
	case json.Delim('['):
		err = a.decodeArray(ctx, d, false)
	case json.Delim('{'):
		fields := map[string]json.RawMessage{}
		envelope := false
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return jsonArchiveError(e)
			}
			k, ok := key.(string)
			if !ok {
				return archiveError("invalid_json")
			}
			if k == "conversations" || k == "records" {
				token, e = d.Token()
				if e != nil {
					return jsonArchiveError(e)
				}
				if token != json.Delim('[') {
					return archiveError("invalid_json")
				}
				envelope = true
				err = a.decodeArray(ctx, d, k == "records")
				if err != nil {
					return err
				}
			} else {
				switch k {
				case "id", "uuid", "title", "name", "current_node", "mapping", "chat_messages", "text", "version", "role", "expressed_at", "conversation_id", "parent_id", "branch", "media_type", "episode_key", "episode_title", "missing_attachments":
					var value json.RawMessage
					if e = d.Decode(&value); e != nil {
						return jsonArchiveError(e)
					}
					fields[k] = value
				default:
					if e = skipArchiveValue(d); e != nil {
						return jsonArchiveError(e)
					}
				}
			}
		}
		token, e := d.Token()
		if e != nil {
			return jsonArchiveError(e)
		}
		if token != json.Delim('}') {
			return archiveError("invalid_json")
		}
		if !envelope {
			raw, _ := json.Marshal(fields)
			err = a.decodeEntry(raw, false)
		}
	default:
		return archiveError("unsupported_archive")
	}
	if err != nil {
		return err
	}
	var extra json.RawMessage
	if err = d.Decode(&extra); err != io.EOF {
		if err == nil {
			return archiveError("invalid_json")
		}
		return jsonArchiveError(err)
	}
	return nil
}
func (a *Archive) decodeArray(ctx context.Context, d *json.Decoder, records bool) error {
	for d.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return jsonArchiveError(err)
		}
		if err := a.decodeEntry(raw, records); err != nil {
			return err
		}
	}
	token, err := d.Token()
	if err != nil {
		return jsonArchiveError(err)
	}
	if token != json.Delim(']') {
		return archiveError("invalid_json")
	}
	return nil
}
func (a *Archive) decodeEntry(raw json.RawMessage, records bool) error {
	if records {
		var r Record
		if err := json.Unmarshal(raw, &r); err != nil {
			return archiveError("invalid_json")
		}
		b, err := Normalize(Batch{Records: []Record{r}})
		if err != nil {
			return archiveError(err.Error())
		}
		return a.add(b)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return archiveError("invalid_json")
	}
	if fields == nil {
		return archiveError("unsupported_archive")
	}
	if fields["mapping"] != nil || fields["chat_messages"] != nil {
		a.Preview.Conversations++
	}
	// The legacy conversation decoder preserves branch traversal, roles and the
	// exact record version fingerprint. It sees just one conversation here.
	b, err := decodeLegacyDocument("conversation.json", raw)
	if err != nil {
		if err.Error() == "invalid_record_count" {
			return nil
		} // an empty exported conversation
		if err.Error() == "unsupported_record" {
			return archiveError("unsupported_archive")
		}
		return archiveError(err.Error())
	}
	return a.add(b)
}
func skipArchiveValue(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '[' && delim != '{' {
		return fmt.Errorf("invalid_json")
	}
	for d.More() {
		if delim == '{' {
			if _, err = d.Token(); err != nil {
				return err
			}
		}
		if err = skipArchiveValue(d); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

// DecodeArchive remains compatible for the connector paths that need a Batch.
// Large uploads and background imports use OpenArchive and read only a chunk.
func DecodeArchive(name string, data []byte) (Batch, error) {
	a, err := OpenArchive(context.Background(), name, bytes.NewReader(data))
	if err != nil {
		return Batch{}, err
	}
	defer a.Close()
	records, err := a.Records(0, a.Len())
	if err != nil {
		return Batch{}, err
	}
	return Normalize(Batch{Records: records, Gaps: a.Preview.Gaps})
}

type ArchiveIdentity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

const blockedArchiveGap = "已跳过你设置为不再导入的消息。"

func (a *Archive) keep(e archiveEntry) {
	if len(a.entries) < MaxArchiveRecords {
		heap.Push(&a.entries, e)
	} else if len(a.entries) > 0 && olderEntry(a.entries[0], e) {
		a.entries[0] = e
		heap.Fix(&a.entries, 0)
	}
}
func (a *Archive) sortEntries() {
	sort.Slice(a.entries, func(i, j int) bool {
		x, y := a.entries[i], a.entries[j]
		if !x.conversationAt.Equal(y.conversationAt) {
			return x.conversationAt.After(y.conversationAt)
		}
		return olderEntry(y, x)
	})
}
func (a *Archive) noteBlocked() {
	if a.Preview.Blocked == 0 {
		return
	}
	for _, gap := range a.Preview.Gaps {
		if gap == blockedArchiveGap {
			return
		}
	}
	if len(a.Preview.Gaps) < 100 {
		a.Preview.Gaps = append(a.Preview.Gaps, blockedArchiveGap)
	} else {
		a.Preview.Gaps[99] = blockedArchiveGap
	}
}

// Select applies identity policy checks only to the messages already retained
// by the newest-message cap. Blocked messages never refill from LeftOut.
func (a *Archive) Select(ctx context.Context, allowed func([]ArchiveIdentity) ([]bool, error)) error {
	a.Preview.Blocked = 0
	err := a.Filter(ctx, func(ids []ArchiveIdentity) ([]bool, error) {
		keep, err := allowed(ids)
		if err == nil {
			for _, ok := range keep {
				if !ok {
					a.Preview.Blocked++
				}
			}
		}
		return keep, err
	})
	a.noteBlocked()
	return err
}

// Filter retains selected identities in their existing order without changing
// preview categories. A worker uses it to omit messages already linked to this
// archive, reconstructing durable progress without using Stored as an offset.
func (a *Archive) Filter(ctx context.Context, allowed func([]ArchiveIdentity) ([]bool, error)) error {
	identities := make([]ArchiveIdentity, 0, 500)
	entries := make([]archiveEntry, 0, 500)
	write := 0
	flush := func() error {
		if len(identities) == 0 {
			return nil
		}
		keep, err := allowed(identities)
		if err != nil {
			return err
		}
		if len(keep) != len(entries) {
			return archiveError("invalid_metadata")
		}
		for i, e := range entries {
			if keep[i] {
				a.entries[write] = e
				write++
			}
		}
		identities = identities[:0]
		entries = entries[:0]
		return nil
	}
	for _, e := range a.entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		data := make([]byte, e.size)
		if _, err := a.spool.ReadAt(data, e.offset); err != nil {
			return err
		}
		var r Record
		if err := json.Unmarshal(data, &r); err != nil {
			return err
		}
		entries = append(entries, e)
		identities = append(identities, ArchiveIdentity{ID: r.ID, Version: r.Version})
		if len(identities) == cap(identities) {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	a.entries = a.entries[:write]
	return nil
}

// Drop accounts for identities blocked after the initial policy snapshot. The
// retained-message cap and LeftOut are unchanged.
func (a *Archive) Drop(start int, allowed []bool) error {
	if start < 0 || start+len(allowed) > len(a.entries) {
		return archiveError("invalid_record_count")
	}
	write := start
	for i, keep := range allowed {
		if keep {
			a.entries[write] = a.entries[start+i]
			write++
		} else {
			a.Preview.Blocked++
		}
	}
	write += copy(a.entries[write:], a.entries[start+len(allowed):])
	a.entries = a.entries[:write]
	a.noteBlocked()
	return nil
}
