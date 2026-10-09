package main

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/memory"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleCopy() (copyManifest, containerInspection) {
	c := copyManifest{Container: strings.Repeat("a", 64), Name: "fictional-copy", Identity: string(memory.NewID()), DatabaseURL: "postgres://pcas:fictional@127.0.0.1:12345/pcas_replay?sslmode=disable"}
	i := containerInspection{ID: c.Container, Name: "/" + c.Name}
	i.State.Running = true
	i.Config.Labels = map[string]string{"pcas.executor": "sol", "pcas.replay.identity": c.Identity}
	i.NetworkSettings.Ports = map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string
	}{"5432/tcp": {{HostIP: "127.0.0.1", HostPort: "12345"}}}
	return c, i
}
func TestCopyIsolationRejectsIdentityEndpointAndProductionDatabase(t *testing.T) {
	for _, change := range []string{"container", "identity", "stopped", "port", "public_bind", "database", "remote"} {
		t.Run(change, func(t *testing.T) {
			c, i := sampleCopy()
			switch change {
			case "container":
				i.ID = strings.Repeat("b", 64)
			case "identity":
				i.Config.Labels["pcas.replay.identity"] = "different"
			case "stopped":
				i.State.Running = false
			case "port":
				i.NetworkSettings.Ports["5432/tcp"][0].HostPort = "5432"
			case "public_bind":
				i.NetworkSettings.Ports["5432/tcp"][0].HostIP = "0.0.0.0"
			case "database":
				c.DatabaseURL = "postgres://pcas:fictional@127.0.0.1:12345/pcas"
			case "remote":
				c.DatabaseURL = "postgres://pcas:fictional@database:12345/pcas_replay"
			}
			if err := matchCopy(c, i); err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
	c, i := sampleCopy()
	if err := matchCopy(c, i); err != nil {
		t.Fatal(err)
	}
}
func TestCaseValidationRejectsUnknownRequestsAndUnboundJobs(t *testing.T) {
	base := caseManifest{Version: 1, CaseID: "fictional", Mode: "record", CallLimit: 1, RealBinary: "fictional", Operations: []operation{{ID: "read", Kind: "secretary", Request: []byte(`{"text":"fictional"}`)}}}
	if err := validateCase(base); err != nil {
		t.Fatal(err)
	}
	for _, op := range []operation{{ID: "bad", Kind: "secretary", Request: []byte(`{"unknown":true}`)}, {ID: "bad", Kind: "background", Stage: "source.extract", SourceFrom: "not-ingested", LeaseToken: memory.NewID()}, {ID: "bad", Kind: "background", Stage: "unknown", JobID: memory.NewID(), LeaseToken: memory.NewID()}, {ID: "bad", Kind: "secretary", Request: []byte(`{} {}`)}} {
		base.Operations = []operation{op}
		if err := validateCase(base); err == nil {
			t.Fatal("unsafe operation accepted", op.Kind)
		}
	}
}

func TestDeputyReplayRequiresOneExplicitRunWithoutOtherOperations(t *testing.T) {
	base := caseManifest{Version: 1, CaseID: "fictional", Mode: "record", CallLimit: 1, RealBinary: "fictional", Operations: []operation{{ID: "draft", Kind: "deputy", RunID: memory.NewID()}}}
	if err := validateCase(base); err != nil {
		t.Fatal(err)
	}
	for _, op := range []operation{
		{ID: "draft", Kind: "deputy"},
		{ID: "draft", Kind: "deputy", RunID: memory.ID("invalid")},
		{ID: "draft", Kind: "deputy", RunID: memory.NewID(), Request: []byte(`{}`)},
		{ID: "draft", Kind: "deputy", RunID: memory.NewID(), JobID: memory.NewID()},
		{ID: "draft", Kind: "deputy", RunID: memory.NewID(), LeaseToken: memory.NewID()},
		{ID: "draft", Kind: "secretary", RunID: memory.NewID(), Request: []byte(`{"text":"fictional"}`)},
		{ID: "draft", Kind: "deputy", RunID: memory.NewID(), MemoryTier: "light"},
	} {
		base.Operations = []operation{op}
		if err := validateCase(base); err == nil {
			t.Fatal("deputy replay accepted an unbound operation", op.Kind)
		}
	}
}

func TestReplayTierOverrideUsesExistingAdmissionModes(t *testing.T) {
	base := caseManifest{Version: 1, CaseID: "fictional", Mode: "record", CallLimit: 1, RealBinary: "fictional"}
	for _, tier := range []string{"light", "medium", "heavy"} {
		base.Operations = []operation{{ID: "admit", Kind: "command", MemoryTier: tier, Request: []byte(`{"type":"requestRun","thingId":"fictional","agentId":"model","kind":"draft","prompt":"fictional"}`)}}
		if err := validateCase(base); err != nil {
			t.Fatal(tier, err)
		}
	}
	for _, op := range []operation{
		{ID: "admit", Kind: "command", MemoryTier: "light", Request: []byte(`{"type":"addTask","title":"fictional"}`)},
		{ID: "admit", Kind: "secretary", MemoryTier: "invalid", Request: []byte(`{"text":"fictional"}`)},
		{ID: "admit", Kind: "ingest", MemoryTier: "light", Request: []byte(`{}`)},
	} {
		base.Operations = []operation{op}
		if err := validateCase(base); err == nil {
			t.Fatal("irrelevant or unknown tier was accepted")
		}
	}
}

// Private local evidence only. Ordinary CI has no production-copy manifest.
func TestOwnedCopyRejectsArtifactsOutsideItsActualContainerMount(t *testing.T) {
	path := os.Getenv("PCAS_REPLAY_PRIVATE_MANIFEST")
	if path == "" {
		t.Skip("owned private copy is not supplied")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := ownedCopy(ctx, path, "pcas_replay_extraction_replay")
	if err != nil {
		t.Fatal("verified private target unavailable", errorCategory(err))
	}
	if _, err := ownedPrivatePath(c.root, filepath.Join(c.root, "future-case", "files")); err != nil {
		t.Fatal(err)
	}
	displaced := filepath.Join(t.TempDir(), "copy-runtime.json")
	if err := writePrivate(displaced, c); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ownedCopy(ctx, displaced, ""); err == nil {
		t.Fatal("private writes could use a root not bound to the container")
	}
}
