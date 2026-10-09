package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
)

type copyManifest struct {
	root           string
	Identity       string          `json:"identity"`
	Container      string          `json:"container"`
	Network        string          `json:"network"`
	Name           string          `json:"name"`
	SourceBackup   string          `json:"sourceBackup"`
	BackupSHA256   string          `json:"backupSHA256"`
	BaseRevision   string          `json:"baseRevision"`
	DatabaseURL    string          `json:"databaseURL"`
	Owner          memory.ID       `json:"owner"`
	Restored       bool            `json:"restored"`
	FilesRestored  bool            `json:"filesRestored"`
	Migrated       bool            `json:"migrated"`
	BaselineCounts json.RawMessage `json:"baselineCounts"`
	Usage          string          `json:"usage"`
}

type containerInspection struct {
	ID     string `json:"Id"`
	Name   string
	State  struct{ Running bool }
	Config struct{ Labels map[string]string }
	Mounts []struct {
		Type        string
		Source      string
		Destination string
	}
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string
		}
	}
}

func ownedCopy(ctx context.Context, path, database string) (copyManifest, string, error) {
	var copy copyManifest
	if err := readPrivate(path, &copy); err != nil {
		return copy, "", err
	}
	if !memory.ID(copy.Identity).Valid() || !copy.Owner.Valid() || len(copy.Container) != 64 || !copy.Restored || !copy.Migrated || !copy.FilesRestored {
		return copy, "", errors.New("copy_manifest_incomplete")
	}
	if _, err := hex.DecodeString(copy.Container); err != nil {
		return copy, "", errors.New("copy_container_id_invalid")
	}
	command := exec.CommandContext(ctx, "docker", "inspect", copy.Container)
	raw, err := command.Output()
	if err != nil {
		return copy, "", errors.New("copy_container_unavailable")
	}
	var containers []containerInspection
	if json.Unmarshal(raw, &containers) != nil || len(containers) != 1 {
		return copy, "", errors.New("copy_inspection_invalid")
	}
	if err := matchCopy(copy, containers[0]); err != nil {
		return copy, "", err
	}
	root, err := privatePath(filepath.Dir(path))
	if err != nil {
		return copy, "", err
	}
	bound := false
	for _, mount := range containers[0].Mounts {
		if mount.Type == "bind" && mount.Destination == "/var/lib/postgresql/data" {
			source, err := privatePath(mount.Source)
			if err != nil {
				return copy, "", err
			}
			bound = source == filepath.Join(root, "database-"+copy.Identity)
		}
	}
	if !bound {
		return copy, "", errors.New("copy_private_root_not_owned")
	}
	copy.root = root
	u, err := url.Parse(copy.DatabaseURL)
	if err != nil {
		return copy, "", errors.New("copy_database_invalid")
	}
	if database != "" {
		// Clones stay on the verified instance. An explicit owned name prevents an
		// ordinary product database from becoming a reset or execution target.
		if !strings.HasPrefix(database, "pcas_replay_") {
			return copy, "", errors.New("copy_clone_name_invalid")
		}
		for _, ch := range database {
			if ch != '_' && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
				return copy, "", errors.New("copy_clone_name_invalid")
			}
		}
		u.Path = "/" + database
	}
	return copy, u.String(), nil
}

func ownedPrivatePath(root, path string) (string, error) {
	resolved, err := privatePath(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("artifact_outside_owned_copy")
	}
	return resolved, nil
}

func matchCopy(copy copyManifest, container containerInspection) error {
	if container.ID != copy.Container || container.Name != "/"+copy.Name || !container.State.Running || container.Config.Labels["pcas.executor"] != "sol" || container.Config.Labels["pcas.replay.identity"] != copy.Identity {
		return errors.New("copy_container_identity_mismatch")
	}
	u, err := url.Parse(copy.DatabaseURL)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Path != "/pcas_replay" || u.User == nil || u.User.Username() != "pcas" {
		return errors.New("copy_database_invalid")
	}
	ports := container.NetworkSettings.Ports["5432/tcp"]
	if len(ports) != 1 || ports[0].HostIP != "127.0.0.1" || ports[0].HostPort != u.Port() {
		return errors.New("copy_database_endpoint_mismatch")
	}
	return nil
}
