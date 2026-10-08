package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/soaringjerry/PCAS/internal/worker"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type phase3File struct {
	SourceID, ProjectID, Name, MediaType, CreatedAt, Status, FailureReason, OpenURL string
	Size                                                                            int64
	JobID                                                                           *string
}
type phase3FileUpload struct {
	File          phase3File
	AlreadyExists bool
}

func (h *phase3HTTP) upload(f *phase3LoadedFixture, item, name string, data []byte) (int, phase3FileUpload, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return 0, phase3FileUpload{}, err
	}
	if _, err := part.Write(data); err != nil {
		return 0, phase3FileUpload{}, err
	}
	if err := writer.Close(); err != nil {
		return 0, phase3FileUpload{}, err
	}
	req, err := http.NewRequestWithContext(f.Context, "POST", h.Base+"/v1/workspace/items/"+item+"/files", &body)
	if err != nil {
		return 0, phase3FileUpload{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer phase3-fictitious")
	response, err := h.Client.Do(req)
	if err != nil {
		return 0, phase3FileUpload{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, phase3FileUpload{}, err
	}
	var out phase3FileUpload
	if response.StatusCode == 200 || response.StatusCode == 201 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return response.StatusCode, out, err
		}
	}
	return response.StatusCode, out, nil
}
func TestPhase3F1ContentDuplicateScopeAndConcurrency(t *testing.T) {
	phase3Finding(t, "S-P3-004")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	p := f.Gold.Projects[0]
	data := []byte("虚构文件：青湾仓库已确认，预算200单位。")
	code, first, err := h.upload(f, p.ID, "虚构资料.txt", data)
	if err != nil || code != 201 || first.AlreadyExists || first.File.ProjectID != p.ID {
		t.Fatal("new project upload", code, first, err)
	}
	var wg sync.WaitGroup
	answers := make(chan phase3FileUpload, 2)
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, out, err := h.upload(f, p.Items[0].ID, fmt.Sprintf("虚构改名%d.txt", i), data)
			if err != nil || code != 200 {
				errors <- fmt.Errorf("duplicate status=%d err=%v", code, err)
				return
			}
			answers <- out
		}(i)
	}
	wg.Wait()
	close(answers)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	for out := range answers {
		if !out.AlreadyExists || out.File.SourceID != first.File.SourceID || out.File.ProjectID != p.ID {
			t.Fatal("same content duplicated or lost project", out)
		}
	}
	var list struct{ Items []phase3File }
	h.get(t, f.Context, "/v1/workspace/items/"+p.ID+"/files", &list)
	if len(list.Items) != 1 {
		t.Fatalf("file source count=%d", len(list.Items))
	}
}
func TestPhase3F1AttachmentSizeBoundary(t *testing.T) {
	phase3Finding(t, "S-P3-004")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	p := f.Gold.Projects[0]
	// Value is the existing limit recorded in skeleton PR #262, not a new product cap.
	const maximum = 20 << 20
	for _, n := range []int{maximum - 1, maximum, maximum + 1} {
		data := bytes.Repeat([]byte("x"), n)
		code, out, err := h.upload(f, p.ID, fmt.Sprintf("虚构大小边界%d.txt", n), data)
		if err != nil {
			t.Fatal(err)
		}
		if n <= maximum {
			if code != 201 || out.File.Size != int64(n) {
				t.Error("in-limit rejected/size changed", n, code, out)
			}
		} else if code < 400 || code >= 500 {
			t.Error("over-limit has no explicit client rejection", code)
		}
	}
}
func TestPhase3F4ConfirmationDeletionPropagationAndBlockedReplay(t *testing.T) {
	phase3Finding(t, "S-P3-004")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	p := f.Gold.Projects[0]
	data := []byte("虚构待删除文件：不要保留任何派生副本。")
	code, out, err := h.upload(f, p.ID, "虚构删除.txt", data)
	if err != nil || code != 201 {
		t.Fatal("upload", code, err)
	}
	path := "/v1/workspace/items/" + p.ID + "/files/" + out.File.SourceID
	code, _, err = h.call(f.Context, "DELETE", path, nil)
	if err != nil || code < 400 {
		t.Fatal("file deleted without explicit confirmation", code, err)
	}
	var listed struct{ Items []phase3File }
	h.get(t, f.Context, "/v1/workspace/items/"+p.ID+"/files", &listed)
	if len(listed.Items) != 1 {
		t.Fatal("cancel/unconfirmed delete lost file")
	}
	code, _, err = h.call(f.Context, "DELETE", path+"?confirmed=true", nil)
	if err != nil || code != 200 {
		t.Fatal("confirmed delete", code, err)
	}
	h.get(t, f.Context, "/v1/workspace/items/"+p.ID+"/files", &listed)
	if len(listed.Items) != 0 {
		t.Fatal("deleted file remains listed")
	}
	code, _, err = h.call(f.Context, "GET", out.File.OpenURL, nil)
	if err != nil || code != 404 {
		t.Fatal("deleted original still accessible", code, err)
	}
	var remains int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT (SELECT count(*) FROM source_versions WHERE owner_id=$1 AND (source_id=$2 OR derived_from_id=$2))+(SELECT count(*) FROM chunks WHERE owner_id=$1 AND source_id=$2)`, f.Scope.OwnerID, out.File.SourceID).Scan(&remains); err != nil || remains != 0 {
		t.Fatal("derived copies/original retained", remains, err)
	}
	code, _, err = h.upload(f, p.ID, "虚构重新导入.txt", data)
	if err != nil {
		t.Fatal(err)
	}
	if code < 400 {
		t.Fatal("deleted content silently reimported", code)
	}
}
func TestPhase3F3F5FileStatusRetryAndInlineDisposition(t *testing.T) {
	phase3Finding(t, "S-P3-004")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	p := f.Gold.Projects[0]
	seed := f.Gold.Projects[1].Files[0]
	data := append(append([]byte{}, seed.Bytes...), []byte("fictitious-new-status-retry-input")...)
	code, out, err := h.upload(f, p.ID, "虚构新状态重试.png", data)
	if err != nil || code != 201 {
		t.Fatal(code, err)
	}
	if out.File.Status != "stored" || out.File.JobID == nil {
		t.Fatal("new file missing real processing status/job", out)
	}
	req, err := http.NewRequestWithContext(f.Context, "GET", h.Base+out.File.OpenURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := h.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !bytes.Equal(body, data) || !strings.HasPrefix(response.Header.Get("Content-Disposition"), "inline") {
		t.Fatal("image open changed bytes/not inline", err, response.StatusCode, response.Header)
	}
	// Explicit failed-job precondition. This case tests list/retry routing;
	// the separate overlap case exercises the actual successful extraction path.
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE memory_jobs SET state='failed',error_code='fictitious_parser_unavailable' WHERE id=$1`, *out.File.JobID)
	var list struct{ Items []phase3File }
	h.get(t, f.Context, "/v1/workspace/items/"+p.ID+"/files", &list)
	found := false
	for _, file := range list.Items {
		if file.SourceID == out.File.SourceID {
			found = true
			if file.Status != "failed" || file.FailureReason == "" {
				t.Fatal("failure not shown", file)
			}
		}
	}
	if !found {
		t.Fatal("file disappeared on failure")
	}
	h.command(t, f.Context, map[string]any{"type": "retryJob", "id": *out.File.JobID})
	var state string
	if err := f.Store.pool.QueryRow(f.Context, `SELECT state FROM memory_jobs WHERE id=$1`, *out.File.JobID).Scan(&state); err != nil || state != "queued" {
		t.Fatal("retry changed identity/lost queue", state, err)
	}
}

func TestPhase3F2T2FileExtractionConcurrentWithSchedulingAndUser(t *testing.T) {
	phase3Finding(t, "S-P3-004")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	p := f.Gold.Projects[0]
	const text = "虚构文件原话：青湾仓库已确认，预算为200虚构单位。"
	code, uploaded, err := h.upload(f, p.ID, "虚构抽取并发.txt", []byte(text))
	if err != nil || code != 201 {
		t.Fatal(code, err)
	}
	m := phase26NewModel(t, f.phase26LoadedFixture)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	isExtraction := func(call phase26Call) bool {
		return strings.Contains(call.System, "predicate") && strings.Contains(call.System, "quote")
	}
	m.mu.Lock()
	m.Before = func(ctx context.Context, call phase26Call) {
		if isExtraction(call) {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}
	m.Reply = func(call phase26Call) string {
		if isExtraction(call) {
			return `{"items":[{"kind":"memory","text":"虚构文件原话：青湾仓库已确认，预算为200虚构单位。","nature":"fact","subject":"用户","predicate":"fictitious_file_fact","quote":"虚构文件原话：青湾仓库已确认，预算为200虚构单位。","confidence":0.99,"acquisition":"direct"}]}`
		}
		return m.goldReply(call)
	}
	m.mu.Unlock()
	// Keep real source pipeline ordering; postpone only unrelated fixture jobs.
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE memory_jobs SET available_at=now()+interval '1 day' WHERE state='queued' AND record_id<>$1`, uploaded.File.SourceID)
	var extraction worker.Job
	found := false
	for i := 0; i < 12; i++ {
		j, err := f.Store.Claim(f.Context, 5*time.Minute)
		if err != nil || j == nil {
			t.Fatal("file pipeline lost work", err)
		}
		switch j.Stage {
		case "source.parse":
			err = f.Store.ProcessAttachment(f.Context, *j)
		case "source.chunk":
			err = f.Store.ProcessChunks(f.Context, *j)
		case "source.extract":
			extraction = *j
			found = true
		default:
			err = f.Store.Defer(f.Context, *j, "phase3_unrelated_source_stage", time.Now().Add(time.Hour), true)
		}
		if err != nil {
			t.Fatal(err)
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("uploaded source did not reach real extraction")
	}
	done := make(chan error, 1)
	go func() { done <- f.Store.ProcessExtraction(f.Context, extraction) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatal("no actual extraction overlap", err)
	case <-time.After(20 * time.Second):
		close(release)
		t.Fatal("extraction barrier")
	}
	scheduled := make(chan error, 1)
	go func() { _, err := f.Store.ScheduleOrganize(f.Context, time.Now()); scheduled <- err }()
	var raw json.RawMessage
	h.get(t, f.Context, "/v1/workspace", &raw)
	h.command(t, f.Context, map[string]any{"type": "addTask", "title": "虚构抽取期间用户事项", "projectId": p.ID})
	if err := <-scheduled; err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM claim_revisions WHERE owner_id=$1 AND scope->>'project_id'=$2 AND value #>> '{}'=$3`, f.Scope.OwnerID, p.ID, text).Scan(&count); err != nil || count != 1 {
		t.Fatal("file text lost/duplicated/project scope absent", count, err)
	}
	var current phase3Handover
	h.get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &current)
	if !current.Stale {
		t.Fatal("file memory did not stale project handover")
	}
}

func TestPhase3F5InlineImagePDFTextAndOtherDownload(t *testing.T) {
	phase3Finding(t, "S-P3-004")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	for _, input := range []struct {
		name   string
		data   []byte
		inline bool
	}{
		{"虚构页内文本.txt", []byte("虚构页内资料：青湾预算200。"), true},
		{"虚构页内图片.png", append(append([]byte{}, f.Gold.Projects[1].Files[0].Bytes...), []byte("fictitious-inline-image")...), true},
		{"虚构页内资料.pdf", []byte("%PDF-1.4\n% fictitious preview\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n"), true},
		{"虚构下载.bin", []byte{0, 1, 2, 3, 4, 255}, false},
	} {
		code, uploaded, err := h.upload(f, f.Gold.Projects[0].ID, input.name, input.data)
		if err != nil || code != 201 {
			t.Fatal("preview input upload", input.name, code, err)
		}
		req, err := http.NewRequestWithContext(f.Context, "GET", h.Base+uploaded.File.OpenURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := h.Client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		wantDisposition := "attachment"
		if input.inline {
			wantDisposition = "inline"
		}
		if readErr != nil || response.StatusCode != 200 || !bytes.Equal(body, input.data) || !strings.HasPrefix(response.Header.Get("Content-Disposition"), wantDisposition) {
			t.Fatal("open/preview byte or disposition mismatch", input.name, response.StatusCode, response.Header, readErr)
		}
	}
}
