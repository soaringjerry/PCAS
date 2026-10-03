//go:build linux

package ai

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func codexProcessFixture(t *testing.T) (*Codex, string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required for process-tree fixture")
	}
	dir := t.TempDir()
	child := filepath.Join(dir, "server.py")
	script := `import os,sys,json,subprocess,pathlib
home=pathlib.Path(os.environ['CODEX_HOME'])
leaf=subprocess.Popen([sys.executable,'-c','import time;time.sleep(120)'])
(home/'pids').write_text(str(os.getppid())+' '+str(os.getpid())+' '+str(leaf.pid))
def emit(x): print(json.dumps(x),flush=True)
for line in sys.stdin:
 m=json.loads(line)
 if 'id' not in m: continue
 method=m.get('method');p=m.get('params',{});result={}
 if method=='account/read': result={'account':{'type':'chatgpt'}}
 if method=='thread/start': result={'thread':{'id':'thread'}}
 if method=='turn/start': result={'turn':{'id':'turn'}}
 emit({'id':m['id'],'result':result})
 if method=='turn/start':
  if p['input'][0]['text']=='hang': (home/'started').write_text('yes')
  else:
   emit({'method':'item/completed','params':{'threadId':'thread','item':{'type':'agentMessage','text':'ready'}}})
   emit({'method':'turn/completed','params':{'threadId':'thread','turn':{'status':'completed'}}})
`
	if err := os.WriteFile(child, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "entry")
	wrapper := "#!/usr/bin/env python3\nimport subprocess,sys\nsubprocess.Popen([sys.executable," + strconv.Quote(child) + "]).wait()\n"
	if err := os.WriteFile(entry, []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := NewCodex(entry, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, pid := range codexFixturePIDs(t, dir) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		c.Close()
	})
	return c, dir
}

func codexFixturePIDs(t *testing.T, dir string) []int {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(dir, "pids"))
	var pids []int
	for _, field := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid < 2 {
			t.Fatal("invalid fixture PID")
		}
		pids = append(pids, pid)
	}
	return pids
}

func codexProcessAlive(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return !os.IsNotExist(err)
	}
	fields := strings.Fields(string(data)[strings.LastIndex(string(data), ")")+1:])
	return len(fields) > 0 && fields[0] != "Z"
}

func assertCodexTreeStopped(t *testing.T, c *Codex, cmd *exec.Cmd, dir string) {
	t.Helper()
	pids := codexFixturePIDs(t, dir)
	if len(pids) != 3 {
		t.Fatalf("expected wrapper, server and descendant, got %v", pids)
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, pid := range pids {
		stopped := false
		for time.Now().Before(deadline) {
			if !codexProcessAlive(pid) {
				stopped = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !stopped {
			t.Fatalf("descendant %d remains alive after Close", pid)
		}
	}
	select {
	case <-c.done:
	default:
		t.Fatal("RPC reader did not finish")
	}
	if cmd.ProcessState == nil {
		t.Fatal("entry point was not reaped before Close returned")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil || c.input != nil || c.output != nil {
		t.Fatal("closed connection retained")
	}
}

func TestCodexCloseReapsWrapperAndDescendants(t *testing.T) {
	c, dir := codexProcessFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Account(ctx); err != nil {
		t.Fatal(err)
	}
	cmd := c.cmd
	// Reproduce the old shutdown: only the entry point exits, leaving the
	// server and its descendant alive and holding the inherited stdout pipe.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	pids := codexFixturePIDs(t, dir)
	if len(pids) != 3 || !codexProcessAlive(pids[1]) || !codexProcessAlive(pids[2]) {
		t.Fatal("fixture did not reproduce an orphaned app-server")
	}
	c.Close()
	assertCodexTreeStopped(t, c, cmd, dir)
	c.Close() // repeated close must not signal a reused process identity
}

func TestCodexReloadSubscriptionDoesNotAccumulateProcesses(t *testing.T) {
	c, dir := codexProcessFixture(t)
	r := &Registry{Codex: c, ReloadSubscription: true, Config: Configuration{Providers: []Provider{{ID: "codex", Protocol: "codex"}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i := 0; i < 20; i++ {
		if _, err := c.Account(ctx); err != nil {
			t.Fatal(err)
		}
		cmd := c.cmd
		result, err := r.Generate(ctx, "codex", "system", "prompt")
		if err != nil || result.Text != "ready" {
			t.Fatal(i, result, err)
		}
		assertCodexTreeStopped(t, c, cmd, dir)
	}
}

func TestCodexCancelledReloadReapsProcessTree(t *testing.T) {
	c, dir := codexProcessFixture(t)
	r := &Registry{Codex: c, ReloadSubscription: true, Config: Configuration{Providers: []Provider{{ID: "codex", Protocol: "codex"}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Account(ctx); err != nil {
		t.Fatal(err)
	}
	cmd := c.cmd
	done := make(chan error, 1)
	go func() { _, err := r.Generate(ctx, "codex", "system", "hang"); done <- err }()
	for {
		if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("turn never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertCodexTreeStopped(t, c, cmd, dir)
}
