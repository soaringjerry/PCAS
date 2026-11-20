package connectors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// The limits of one sync: 8 MiB read at a time, 100 records of a poll, a
// cursor or an ETag of 4,096 bytes, 1,000 files and 10,000 records of a
// folder. A sync beyond a limit stops as an invalid source.
const (
	readBytes     = 8 << 20
	pollRecords   = 100
	cursorBytes   = 4096
	folderFiles   = 1000
	folderRecords = 10000
)

// folderExtensions are the files a folder connection reads.
var folderExtensions = map[string]bool{".txt": true, ".md": true, ".json": true, ".jsonl": true, ".zip": true}

// Lease is one connection leased for one sync.
type Lease struct {
	Owner        memory.ID
	Connection   Connection
	Token        memory.ID
	Cursor, ETag string
}

// SyncStore is the storage of one sync.
type SyncStore interface {
	// Finish writes the records of the batch, the fingerprints of the files
	// read, and the next cursor, under the lease.
	Finish(ctx context.Context, l Lease, b Batch, etag string, files map[string]string) error
	// Fingerprint is the fingerprint of the file at the path the last time
	// the connection read it, or empty.
	Fingerprint(ctx context.Context, l Lease, pathHash []byte) (string, error)
	// IngestArchive keeps a whole original export before its messages.
	IngestArchive(ctx context.Context, scope memory.Scope, name string, data []byte, origin string) error
}

// Sync reads a leased connection once: a folder from the inbox directory,
// or a poll over HTTP from its cursor.
func Sync(ctx context.Context, store SyncStore, l Lease) error {
	if l.Connection.Kind == "folder" {
		return syncFolder(ctx, store, l)
	}
	return syncHTTP(ctx, store, l)
}

// SyncError is the code of a failed sync that the connection shows.
func SyncError(err error) string {
	if errors.Is(err, memory.ErrConflict) {
		return "source_version_conflict"
	}
	if errors.Is(err, memory.ErrInvalid) {
		return "invalid_source_format"
	}
	if errors.Is(err, memory.ErrForbidden) {
		return "access_denied"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "sync_timeout"
	}
	return "source_unavailable"
}

func syncHTTP(ctx context.Context, store SyncStore, l Lease) error {
	u, err := url.Parse(l.Connection.URL)
	if err != nil {
		return err
	}
	q := u.Query()
	if l.Cursor != "" {
		q.Set("cursor", l.Cursor)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if l.ETag != "" {
		req.Header.Set("If-None-Match", l.ETag)
	}
	if l.Connection.TokenEnv != "" {
		token := os.Getenv(l.Connection.TokenEnv)
		if token == "" {
			return memory.ErrForbidden
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return store.Finish(ctx, l, Batch{}, l.ETag, nil)
	}
	if response.StatusCode != 200 {
		return memory.ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, readBytes+1))
	if err != nil || len(data) > readBytes {
		return memory.ErrInvalid
	}
	var b Batch
	if json.Unmarshal(data, &b) != nil || len(b.Records) > pollRecords || len(b.NextCursor) > cursorBytes || len(response.Header.Get("ETag")) > cursorBytes {
		return memory.ErrInvalid
	}
	if len(b.Records) > 0 {
		b, err = Normalize(b)
		if err != nil {
			return memory.ErrInvalid
		}
	}
	if b.HasMore && (b.NextCursor == "" || b.NextCursor == l.Cursor) {
		return memory.ErrInvalid
	}
	return store.Finish(ctx, l, b, response.Header.Get("ETag"), nil)
}

func syncFolder(ctx context.Context, store SyncStore, l Lease) error {
	root := os.Getenv("PCAS_INBOX_DIR")
	if root == "" {
		return memory.ErrUnavailable
	}
	// os.Root rejects symlink escapes and traversal, including concurrent changes.
	directory, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err = directory.Mkdir(string(l.Connection.ID), 0700); err != nil && !os.IsExist(err) {
		return err
	}
	folder, err := directory.OpenRoot(string(l.Connection.ID))
	if err != nil {
		return err
	}
	defer folder.Close()
	names, err := folder.Open(".")
	if err != nil {
		return err
	}
	entries, err := names.ReadDir(folderFiles + 1)
	names.Close()
	if err != nil && err != io.EOF {
		return err
	}
	if len(entries) > folderFiles {
		return memory.ErrInvalid
	}
	batch := Batch{Records: []Record{}, Gaps: []string{}}
	fingerprints := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !folderExtensions[ext] {
			continue
		}
		file, err := folder.Open(entry.Name())
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(file, readBytes+1))
		file.Close()
		if err != nil || len(data) > readBytes {
			return memory.ErrInvalid
		}
		sum := sha256.Sum256(data)
		fingerprint := hex.EncodeToString(sum[:])
		pathHash := sha256.Sum256([]byte(entry.Name()))
		previous, err := store.Fingerprint(ctx, l, pathHash[:])
		if err != nil {
			return err
		}
		if previous == fingerprint {
			continue
		}
		decoded, err := DecodeArchive(entry.Name(), data)
		if err != nil {
			return memory.ErrInvalid
		}
		if len(batch.Records)+len(decoded.Records) > folderRecords {
			return memory.ErrInvalid
		}
		// The entire original export is retained before its normalized messages.
		scope := memory.Scope{OwnerID: l.Owner, PrincipalID: "connector", IsOwner: true}
		if ext == ".zip" || ext == ".json" || ext == ".jsonl" {
			if err = store.IngestArchive(ctx, scope, entry.Name(), data, "connection-archive:"+string(l.Connection.ID)); err != nil && !errors.Is(err, memory.ErrBlocked) {
				return err
			}
		} else {
			batch.Records = append(batch.Records, decoded.Records...)
		}
		batch.Gaps = append(batch.Gaps, decoded.Gaps...)
		fingerprints[entry.Name()] = fingerprint
	}
	return store.Finish(ctx, l, batch, "", fingerprints)
}
