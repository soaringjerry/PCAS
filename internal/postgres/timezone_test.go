package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestWorkspaceTimezoneCreationAndSettings(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx, err := workspace.WithInitialTimezone(context.Background(), "Australia/Melbourne")
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.Snapshot(ctx, scope)
	if err != nil || state.Settings.Timezone != "Australia/Melbourne" {
		t.Fatal(state.Settings, err)
	}
	otherBrowser, _ := workspace.WithInitialTimezone(context.Background(), "Asia/Shanghai")
	again, err := s.Snapshot(otherBrowser, scope)
	if err != nil || again.Settings.Timezone != state.Settings.Timezone {
		t.Fatal("existing preference overwritten", again.Settings, err)
	}
	for _, zone := range []any{"Australia/Unknown", "", "Local", nil, 8, []string{"UTC"}} {
		_, err := s.Execute(ctx, scope, workspace.Command{Type: "updateSettings", RequestID: string(memory.NewID()), ExpectedRevision: state.Revision, Patch: asJSON(map[string]any{"timezone": zone, "dailyBudget": 20})})
		if !errors.Is(err, workspace.ErrTimezone) {
			t.Fatalf("%v: %v", zone, err)
		}
		again, err := s.Snapshot(ctx, scope)
		if err != nil || again.Settings != state.Settings || again.Revision != state.Revision {
			t.Fatal("invalid patch partially saved", again.Settings, err)
		}
	}
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "America/New_York"})})
	again, err = s.Snapshot(ctx, scope)
	if err != nil || again.Settings.Timezone != "America/New_York" {
		t.Fatal("timezone did not persist", again.Settings, err)
	}
	noBrowser, err := s.Snapshot(context.Background(), owner())
	if err != nil || noBrowser.Settings.Timezone != "UTC" {
		t.Fatal(noBrowser.Settings, err)
	}
}
