package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func privatePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// Resolve the existing ancestor too: a new file below a symlink into Git
	// must have the same protection as an already existing file.
	ancestor := absolute
	for {
		resolved, e := filepath.EvalSymlinks(ancestor)
		if e == nil {
			relative, err := filepath.Rel(ancestor, absolute)
			if err != nil {
				return "", err
			}
			absolute = filepath.Join(resolved, relative)
			break
		}
		if !errors.Is(e, os.ErrNotExist) || filepath.Dir(ancestor) == ancestor {
			return "", e
		}
		ancestor = filepath.Dir(ancestor)
	}
	for dir := absolute; ; dir = filepath.Dir(dir) {
		if _, e := os.Stat(filepath.Join(dir, ".git")); e == nil {
			return "", errors.New("private_artifact_inside_repository")
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return absolute, nil
}

func readPrivate(path string, value any) error {
	path, err := privatePath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("private_artifact_permissions")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("private_artifact_trailing_data")
	}
	return nil
}

func writePrivate(path string, value any) error {
	path, err := privatePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".replay-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	err = json.NewEncoder(f).Encode(value)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Link publishes a complete file without replacing earlier good evidence.
	return os.Link(f.Name(), path)
}
