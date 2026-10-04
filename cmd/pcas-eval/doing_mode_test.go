package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDoingDispatchKeepsOldModes(t *testing.T) {
	for _, args := range [][]string{{"-mode=comparison"}, {"-mode", "retrieval"}, {"-tier=hard"}} {
		if _, ok := doingModeArgs(args); ok {
			t.Fatal("intercepted old mode")
		}
	}
	for _, args := range [][]string{{"-mode=doing", "-fake"}, {"-mode", "doing", "-fake"}, {"--mode=doing", "-fake"}} {
		got, ok := doingModeArgs(args)
		if !ok || !reflect.DeepEqual(got, []string{"-fake"}) {
			t.Fatal("bad dispatch", got)
		}
	}
}
func TestOutputGuardWorktreesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	os.Mkdir(repo, 0700)
	os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: elsewhere"), 0600)
	if outsideRepository(filepath.Join(repo, "new", "private.json")) == nil {
		t.Fatal("allowed repository output")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	if outsideRepository(filepath.Join(link, "private.json")) == nil {
		t.Fatal("allowed symlink into repository")
	}
	// This machine has a /tmp/.git sentinel; tests should not assume t.TempDir
	// itself is outside a repository.
}
