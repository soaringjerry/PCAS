package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
)

const MaxBytes int64 = 20 << 20

type Files struct{ root string }

func NewFiles(root string) (*Files, error) {
	if root == "" {
		return nil, nil
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	return &Files{root: absolute}, nil
}
func (f *Files) path(scope memory.Scope, key string) (string, error) {
	parts := strings.Split(key, "/")
	if !scope.Valid() || len(parts) != 2 || parts[0] != string(scope.OwnerID) || len(parts[1]) != 97 {
		return "", memory.ErrForbidden
	}
	if _, err := hex.DecodeString(strings.ReplaceAll(parts[1], "-", "")); err != nil {
		return "", memory.ErrInvalid
	}
	return filepath.Join(f.root, parts[0], parts[1]), nil
}
func (f *Files) Put(ctx context.Context, scope memory.Scope, r io.Reader) (string, error) {
	if !scope.Valid() || !scope.IsOwner {
		return "", memory.ErrForbidden
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir := filepath.Join(f.root, string(scope.OwnerID))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "upload-")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return "", err
	}
	if n == 0 || n > MaxBytes {
		return "", memory.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	key := string(scope.OwnerID) + "/" + fmt.Sprintf("%x", hash.Sum(nil)) + "-" + strings.ReplaceAll(string(memory.NewID()), "-", "")
	target, err := f.path(scope, key)
	if err != nil {
		return "", err
	}
	if err := os.Rename(file.Name(), target); err != nil {
		return "", err
	}
	return key, nil
}
func (f *Files) Open(ctx context.Context, scope memory.Scope, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := f.path(scope, key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}
func (f *Files) Delete(ctx context.Context, scope memory.Scope, key string) error {
	if !scope.IsOwner {
		return memory.ErrForbidden
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := f.path(scope, key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
