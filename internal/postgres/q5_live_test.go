package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Explicit opt-in: the DB must be disposable and the Codex home must be a
// dedicated test login, never the production login. Two OS processes reproduce
// serve's persistent app-server and worker's reload-after-each-call behavior.
func TestQ5LiveSecretaryUnderExtractionLoad(t *testing.T) {
	if os.Getenv("PCAS_Q5_LIVE") != "1" || os.Getenv("PCAS_LIVE_CODEX_HOME") == "" {
		t.Skip("requires PCAS_Q5_LIVE=1 and a dedicated PCAS_LIVE_CODEX_HOME")
	}
	s := testStore(t)
	scope := owner()
	models := q5LiveModels(t, false)
	s.SetModels(models)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	dir := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	background := exec.Command(binary, "-test.run=^TestQ5BackgroundProcess$", "-test.v", "-test.timeout=30m")
	// Pass only the isolated schema's connection string and the explicit test
	// login, plus the same non-secret runtime allowlist as the Codex adapter.
	for _, key := range []string{"PATH", "HOME", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "PCAS_LIVE_CODEX_HOME", "PCAS_LIVE_CODEX_BINARY"} {
		if value := os.Getenv(key); value != "" {
			background.Env = append(background.Env, key+"="+value)
		}
	}
	background.Env = append(background.Env, "PCAS_Q5_CHILD=1", "PCAS_Q5_STATE="+dir,
		"PCAS_Q5_DATABASE="+s.pool.Config().ConnString(), "PCAS_Q5_OWNER="+string(scope.OwnerID))
	background.Stdout, background.Stderr = os.Stdout, os.Stderr
	if err := background.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.WriteFile(filepath.Join(dir, "stop"), nil, 0600); err != nil {
			t.Error(err)
		}
		if err := background.Wait(); err != nil {
			t.Error("background process failed", err)
		}
	}()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background extraction did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var conversation *string
	successes := 0
	for i := 1; i <= 10; i++ {
		started := time.Now()
		req := turnRequest(fmt.Sprintf("帮我建一条事项：Q5虚构测试事项%02d。", i))
		req.AgentID, req.ConversationID = "chatgpt", conversation
		out, err := s.DeskTurn(context.Background(), scope, req)
		ok := false
		if err == nil {
			conversation = &out.ConversationID
			for _, receipt := range out.Turn.Receipts {
				if receipt.Op == "create_task" && receipt.Status == "done" {
					ok = true
				}
			}
		}
		if ok {
			successes++
		}
		t.Logf("Q5 foreground sentence=%d success=%t elapsed_ms=%d", i, ok, time.Since(started).Milliseconds())
	}
	t.Logf("Q5 foreground successes=%d/10", successes)
	if os.Getenv("PCAS_Q5_REQUIRE_SUCCESS") == "1" && successes != 10 {
		t.Errorf("secretary succeeded %d/10 times", successes)
	}
}

func TestQ5BackgroundProcess(t *testing.T) {
	if os.Getenv("PCAS_Q5_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	dir := os.Getenv("PCAS_Q5_STATE")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	s, err := Open(ctx, os.Getenv("PCAS_Q5_DATABASE"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetModels(q5LiveModels(t, true))
	scope := memory.Scope{OwnerID: memory.ID(os.Getenv("PCAS_Q5_OWNER")), PrincipalID: "worker", IsOwner: true}
	for i := 1; ctx.Err() == nil; i++ {
		source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "desk", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "Q5虚构原话", Text: fmt.Sprintf("我喜欢清晨读书，这是第%d条虚构测试记录。", i), MediaType: "text/plain"})
		job := leaseStage(t, s, scope, source.Ref, "source.extract")
		if err := os.WriteFile(filepath.Join(dir, "started"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		err := s.ProcessExtraction(ctx, job)
		if ctx.Err() != nil {
			break
		}
		t.Logf("Q5 background extraction=%d success=%t elapsed_ms=%d", i, err == nil, time.Since(started).Milliseconds())
	}
}

func q5LiveModels(t *testing.T, reload bool) *ai.Registry {
	t.Helper()
	c, err := ai.NewCodex(os.Getenv("PCAS_LIVE_CODEX_BINARY"), os.Getenv("PCAS_LIVE_CODEX_HOME"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	models, err := ai.Load("", c)
	if err != nil {
		t.Fatal(err)
	}
	models.Config.Extraction, models.ReloadSubscription = "chatgpt", reload
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	t.Cleanup(func() { slog.SetDefault(prior) })
	return models
}
