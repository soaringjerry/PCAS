// production-replay invokes only listed application operations on a verified
// disposable production copy. It starts no product server or background loops.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type operation struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Request    json.RawMessage `json:"request,omitempty"`
	JobID      memory.ID       `json:"job_id,omitempty"`
	SourceFrom string          `json:"source_from,omitempty"`
	Stage      string          `json:"stage,omitempty"`
	LeaseToken memory.ID       `json:"lease_token,omitempty"`
}

type caseManifest struct {
	Version       int         `json:"version"`
	CaseID        string      `json:"case_id"`
	Revision      string      `json:"revision"`
	CopyManifest  string      `json:"copy_manifest"`
	Database      string      `json:"database"`
	Mode          string      `json:"mode"`
	Models        string      `json:"models"`
	Settings      string      `json:"settings"`
	CodexHome     string      `json:"codex_home"`
	CodexShim     string      `json:"codex_shim"`
	RealBinary    string      `json:"real_binary,omitempty"`
	CallLimit     int         `json:"call_limit"`
	HTTPCallLimit int         `json:"http_call_limit"`
	Output        string      `json:"output"`
	Recording     string      `json:"recording,omitempty"`
	BlobDir       string      `json:"blob_dir"`
	MinimumCalls  int         `json:"minimum_calls"`
	BusinessTime  *time.Time  `json:"business_time,omitempty"`
	Operations    []operation `json:"operations"`
}

type operationResult struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Result      any       `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	ErrorDetail string    `json:"error_detail,omitempty"`
	DurationMS  int64     `json:"duration_ms"`
	StartedAt   time.Time `json:"started_at"`
}

type captureState struct {
	CaseID             string            `json:"case_id"`
	Requests           int               `json:"requests"`
	Cursor             int               `json:"cursor"`
	Completed          int               `json:"completed"`
	LiveProviderStarts int               `json:"live_provider_starts"`
	Suppressed         int               `json:"suppressed"`
	Errors             []json.RawMessage `json:"errors"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "production_replay_failed; inspect private evidence")
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) (runErr error) {
	flags := flag.NewFlagSet("production-replay", flag.ContinueOnError)
	path := flags.String("manifest", "", "private owned-copy operation manifest")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" || flags.NArg() != 0 {
		return errors.New("replay_manifest_required")
	}
	var m caseManifest
	if err := readPrivate(*path, &m); err != nil {
		return err
	}
	if err := validateCase(m); err != nil {
		return err
	}
	copy, dsn, err := ownedCopy(ctx, m.CopyManifest, m.Database)
	if err != nil {
		return err
	}
	output, err := ownedPrivatePath(copy.root, m.Output)
	if err != nil {
		return err
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return err
	}
	report := map[string]any{"case_id": m.CaseID, "mode": m.Mode, "revision": m.Revision, "copy_identity": copy.Identity, "backup_sha256": copy.BackupSHA256, "started_at": time.Now().UTC(), "complete": false}
	report["business_time"] = m.BusinessTime
	defer func() {
		report["finished_at"] = time.Now().UTC()
		if runErr != nil {
			report["error"] = errorCategory(runErr)
			report["error_detail"] = runErr.Error()
			report["complete"] = false
		}
		runErr = errors.Join(runErr, writePrivate(filepath.Join(output, "report.json"), report))
	}()
	for _, path := range []string{m.CodexHome, m.Models, m.Settings, m.BlobDir} {
		if _, err := ownedPrivatePath(copy.root, path); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(m.CodexHome, 0700); err != nil {
		return err
	}
	config := map[string]any{"mode": m.Mode, "case_id": m.CaseID, "call_limit": m.CallLimit, "state_file": filepath.Join(output, "codex-state.json"), "record_file": filepath.Join(output, "codex.jsonl")}
	if m.Mode == "record" {
		config["real_binary"] = m.RealBinary
	}
	// Replay keeps its original cassette path distinct from the new run evidence.
	if m.Mode == "replay" {
		if _, err := ownedPrivatePath(copy.root, m.Recording); err != nil {
			return err
		}
		var completed struct {
			Complete        bool       `json:"complete"`
			CaseID          string     `json:"case_id"`
			Mode            string     `json:"mode"`
			Operations      int        `json:"operations"`
			OperationErrors int        `json:"operation_errors,omitempty"`
			CodexCalls      int        `json:"codex_calls"`
			HTTPCalls       int        `json:"http_calls"`
			BusinessTime    *time.Time `json:"business_time,omitempty"`
		}
		if err := readPrivate(filepath.Join(m.Recording, "completion.json"), &completed); err != nil {
			return err
		}
		if !completed.Complete || completed.CaseID != m.CaseID || completed.Mode != "record" {
			return errors.New("recorded_case_incomplete")
		}
		if (completed.BusinessTime == nil) != (m.BusinessTime == nil) || (m.BusinessTime != nil && !m.BusinessTime.Equal(*completed.BusinessTime)) {
			return errors.New("recorded_business_time_mismatch")
		}
		config["record_file"] = filepath.Join(m.Recording, "codex.jsonl")
	} else {
		file, err := os.OpenFile(config["record_file"].(string), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	if err := writePrivate(filepath.Join(m.CodexHome, ".pcas-replay.json"), config); err != nil {
		return err
	}
	codex, err := ai.NewCodex(m.CodexShim, m.CodexHome)
	if err != nil {
		return err
	}
	defer codex.Close()
	// Load from the copied settings, never an inherited server settings path.
	previous, present := os.LookupEnv("PCAS_MODEL_SETTINGS_FILE")
	if err := os.Setenv("PCAS_MODEL_SETTINGS_FILE", m.Settings); err != nil {
		return err
	}
	models, err := ai.Load(m.Models, codex)
	if present {
		runErr = os.Setenv("PCAS_MODEL_SETTINGS_FILE", previous)
	} else {
		runErr = os.Unsetenv("PCAS_MODEL_SETTINGS_FILE")
	}
	if runErr != nil {
		return runErr
	}
	if err != nil {
		return err
	}
	models.SettingsPath = m.Settings
	for _, provider := range models.Config.Providers {
		if provider.KeyEnv != "" && os.Getenv(provider.KeyEnv) != "" {
			return errors.New("inherited_provider_credentials_not_allowed")
		}
	}
	httpPath := filepath.Join(output, "http.jsonl")
	if m.Mode == "replay" {
		httpPath = filepath.Join(m.Recording, "http.jsonl")
	}
	if m.Mode == "record" {
		file, err := os.OpenFile(httpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	transport, err := newRecordedHTTP(m.Mode, httpPath, http.DefaultTransport)
	if err != nil {
		return err
	}
	transport.limit = m.HTTPCallLimit
	models.HTTP = &http.Client{Transport: transport, Timeout: 3 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	store, err := postgres.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.CheckSchema(ctx); err != nil {
		return err
	}
	store.SetModels(models)
	if m.BusinessTime != nil {
		at := *m.BusinessTime
		store.SetBusinessClock(func() time.Time { return at })
	}
	blobs, err := blob.NewFiles(m.BlobDir)
	if err != nil {
		return err
	}
	store.SetBlobs(blobs)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	scope := memory.Scope{OwnerID: copy.Owner, PrincipalID: "owner", IsOwner: true}
	before, err := snapshot(ctx, pool, copy.Owner, filepath.Join(output, "before.jsonl.gz"))
	if err != nil {
		return err
	}
	report["before"] = before
	results := []operationResult{}
	sources := map[string]memory.Ref{}
	for _, op := range m.Operations {
		transport.selectOperation(op.ID)
		started := time.Now()
		result := operationResult{ID: op.ID, Kind: op.Kind, StartedAt: started.UTC()}
		// This allowance matches Worker.RunOnce. Lease fencing still uses real
		// database time; no complete worker loop runs in this diagnostic command.
		work, cancel := context.WithTimeout(ctx, 4*time.Minute)
		var opErr error
		switch op.Kind {
		case "secretary":
			var request workspace.DeskTurnRequest
			opErr = json.Unmarshal(op.Request, &request)
			if opErr == nil {
				result.Result, opErr = store.DeskTurn(work, scope, request)
			}
		case "ingest":
			var request memory.IngestRequest
			opErr = json.Unmarshal(op.Request, &request)
			if opErr == nil {
				var ingested memory.IngestResult
				ingested, opErr = store.Ingest(work, scope, request)
				if opErr == nil {
					sources[op.ID] = ingested.Ref
					result.Result = ingested
				}
			}
		case "command":
			var request workspace.Command
			opErr = json.Unmarshal(op.Request, &request)
			if opErr == nil {
				result.Result, opErr = store.Execute(work, scope, request)
			}
		case "background":
			var ref memory.Ref
			if op.SourceFrom != "" {
				ref = sources[op.SourceFrom]
			}
			job, err := leaseNamed(work, pool, copy.Owner, op, ref)
			opErr = err
			if err == nil {
				result.Result = job
				opErr = processNamed(work, store, job)
			}
		}
		cancel()
		if opErr != nil {
			result.Error = errorCategory(opErr)
			result.ErrorDetail = opErr.Error()
		}
		result.DurationMS = time.Since(started).Milliseconds()
		results = append(results, result)
		if err := writePrivate(filepath.Join(output, "operation-"+op.ID+".json"), result); err != nil {
			return err
		}
		if ctx.Err() != nil {
			break
		}
	}
	// Stop the provider before reading its durable counters and transcript.
	codex.Close()
	report["operations"] = results
	operationErrors := 0
	for _, result := range results {
		if result.Error != "" {
			operationErrors++
		}
	}
	report["operation_errors"] = operationErrors
	after, err := snapshot(ctx, pool, copy.Owner, filepath.Join(output, "after.jsonl.gz"))
	if err != nil {
		return err
	}
	report["after"] = after
	var state captureState
	statePath := filepath.Join(output, "codex-state.json")
	if _, err := os.Stat(statePath); err == nil {
		if err := readPrivate(statePath, &state); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	report["codex"] = state
	report["http"] = map[string]any{"requests": transport.requests, "live_calls": transport.live, "replies_consumed": transport.cursor, "recorded_replies": len(transport.records), "errors": transport.failures, "suppressed": transport.suppressed}
	report["recorded_model_usage_is_not_replay_spending"] = true
	if state.Suppressed != 0 || len(state.Errors) != 0 || len(transport.failures) != 0 || state.Requests != state.Completed {
		return errors.New("model_recording_incomplete")
	}
	if state.Completed+transport.requests < m.MinimumCalls {
		return errors.New("required_model_calls_missing")
	}
	if m.Mode == "replay" {
		if state.LiveProviderStarts != 0 || transport.live != 0 || transport.cursor != len(transport.records) {
			return errors.New("offline_replay_incomplete")
		}
		raw, err := os.ReadFile(config["record_file"].(string))
		if err != nil {
			return err
		}
		records := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) != "" {
				records++
			}
		}
		if state.Cursor != records {
			return errors.New("unused_recorded_replies")
		}
	}
	report["complete"] = true
	if err := writePrivate(filepath.Join(output, "completion.json"), map[string]any{"complete": true, "case_id": m.CaseID, "mode": m.Mode, "business_time": m.BusinessTime, "operations": len(results), "operation_errors": operationErrors, "codex_calls": state.Completed, "http_calls": transport.requests}); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"case_id": m.CaseID, "mode": m.Mode, "complete": true, "operations": len(results), "operation_errors": operationErrors, "codex_calls": state.Completed, "http_calls": transport.requests, "live_http_calls": transport.live})
}

func validateCase(m caseManifest) error {
	if m.BusinessTime != nil && m.BusinessTime.IsZero() {
		return errors.New("case_business_time_invalid")
	}
	if m.Version != 1 || m.CaseID == "" || (m.Mode != "record" && m.Mode != "replay") || m.MinimumCalls < 0 || len(m.Operations) == 0 || (m.Mode == "record" && (m.CallLimit < 1 || m.RealBinary == "" || m.HTTPCallLimit < 0 || m.Recording != "")) || (m.Mode == "replay" && (m.RealBinary != "" || m.Recording == "")) {
		return errors.New("case_manifest_invalid")
	}
	identifiers := map[string]bool{}
	ingests := map[string]bool{}
	for _, op := range m.Operations {
		if !operationIDValid(op.ID) || identifiers[op.ID] {
			return errors.New("case_operation_identity_invalid")
		}
		identifiers[op.ID] = true
		if op.Kind != "secretary" && op.Kind != "ingest" && op.Kind != "command" && op.Kind != "background" {
			return errors.New("case_operation_unknown")
		}
		if op.Kind == "background" && (!op.LeaseToken.Valid() || (op.JobID == "" && op.SourceFrom == "") || op.Stage == "") {
			return errors.New("case_job_identity_required")
		}
		if op.Kind == "background" {
			if !supportedStage(op.Stage) || (op.JobID != "" && !op.JobID.Valid()) || (op.SourceFrom != "" && (!ingests[op.SourceFrom] || op.JobID != "")) {
				return errors.New("case_job_reference_invalid")
			}
			if len(op.Request) != 0 {
				return errors.New("case_background_request_invalid")
			}
			continue
		}
		var request any
		switch op.Kind {
		case "secretary":
			request = new(workspace.DeskTurnRequest)
		case "ingest":
			request = new(memory.IngestRequest)
			ingests[op.ID] = true
		case "command":
			request = new(workspace.Command)
		}
		decoder := json.NewDecoder(bytes.NewReader(op.Request))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(request); err != nil {
			return errors.New("case_request_invalid")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return errors.New("case_request_trailing_data")
		}
		if op.JobID != "" || op.SourceFrom != "" || op.Stage != "" || op.LeaseToken != "" {
			return errors.New("case_operation_fields_invalid")
		}
	}
	return nil
}

func operationIDValid(id string) bool {
	if id == "" {
		return false
	}
	for _, char := range id {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func errorCategory(err error) string {
	var failure *worker.JobError
	if errors.As(err, &failure) {
		return failure.Code
	}
	if errors.Is(err, worker.ErrLeaseLost) {
		return "lease_lost"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	for name, value := range map[string]error{"invalid": memory.ErrInvalid, "conflict": memory.ErrConflict, "forbidden": memory.ErrForbidden, "not_found": memory.ErrNotFound, "unavailable": memory.ErrUnavailable} {
		if errors.Is(err, value) {
			return name
		}
	}
	return "operation_failed"
}
