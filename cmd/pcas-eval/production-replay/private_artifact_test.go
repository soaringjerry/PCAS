package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateArtifactDoesNotOverwriteGoodEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := writePrivate(path, map[string]string{"value": "good"}); err != nil {
		t.Fatal(err)
	}
	if err := writePrivate(path, map[string]string{"value": "replacement"}); err == nil {
		t.Fatal("good evidence replaced")
	}
	var got map[string]string
	if err := readPrivate(path, &got); err != nil || got["value"] != "good" {
		t.Fatal(got, err)
	}
}
func TestPrivateArtifactRejectsFutureFileThroughRepositorySymlink(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	os.Mkdir(repo, 0700)
	os.Mkdir(filepath.Join(repo, ".git"), 0700)
	link := filepath.Join(root, "alias")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	if _, err := privatePath(filepath.Join(link, "new", "secret.json")); err == nil {
		t.Fatal("future private file in Git accepted")
	}
}
func TestPrivateArtifactRejectsTrailingJSONAndPublicFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, []byte(`{"value":"fictional"} {}`), 0600); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Value string `json:"value"`
	}
	if err := readPrivate(path, &got); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	os.WriteFile(path, []byte(`{"value":"fictional"}`), 0600)
	os.Chmod(path, 0644)
	if err := readPrivate(path, &got); err == nil {
		t.Fatal("public evidence accepted")
	}
}

func TestWritableArtifactsStayInsideOwnedCopy(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if _, err := ownedPrivatePath(root, filepath.Join(outside, "blobs")); err == nil {
		t.Fatal("unowned writable store accepted")
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := ownedPrivatePath(root, filepath.Join(root, "escape", "future-store")); err == nil {
		t.Fatal("symlink escaped the owned copy")
	}
	if _, err := ownedPrivatePath(root, filepath.Join(root, "case", "files")); err != nil {
		t.Fatal(err)
	}
}
