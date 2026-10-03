package httpapi

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// A proxy in front of the server may refuse a large request outright (a
// common limit is 100 MB), so an archive can also be sent in pieces and then
// previewed and imported by name. The pieces are kept on local disk only until
// the import starts, the owner discards them, or they go stale.
const (
	archivePieceBytes  = 8 << 20
	archiveUploadTTL   = 2 * time.Hour
	archiveUploadsOpen = 3
)

type archiveUploadSession struct {
	owner    memory.ID
	name     string
	path     string
	size     int64
	received int64
	touched  time.Time
	busy     bool
}

type archiveUploads struct {
	mu       sync.Mutex
	sessions map[string]*archiveUploadSession
}

var uploads = &archiveUploads{sessions: map[string]*archiveUploadSession{}}

// take returns the owner's session and marks it in use, so two requests never
// write or read one file at once.
func (u *archiveUploads) take(owner memory.ID, id string) (*archiveUploadSession, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	session := u.sessions[id]
	if session == nil || session.owner != owner {
		return nil, memory.ErrNotFound
	}
	if session.busy {
		return nil, memory.ErrConflict
	}
	session.busy, session.touched = true, time.Now()
	return session, nil
}
func (u *archiveUploads) release(session *archiveUploadSession) {
	u.mu.Lock()
	session.busy, session.touched = false, time.Now()
	u.mu.Unlock()
}
func (u *archiveUploads) remove(id string) {
	u.mu.Lock()
	session := u.sessions[id]
	delete(u.sessions, id)
	u.mu.Unlock()
	if session != nil {
		_ = os.Remove(session.path)
	}
}

// open starts a session after dropping stale ones; an owner who already has
// the allowed number open loses the oldest idle one.
func (u *archiveUploads) open(owner memory.ID, name string, size int64) (string, error) {
	file, err := os.CreateTemp("", "pcas-archive-upload-")
	if err != nil {
		return "", err
	}
	_ = file.Close()
	id := string(memory.NewID())
	var stale []string
	u.mu.Lock()
	open, oldest := 0, ""
	for key, session := range u.sessions {
		if !session.busy && time.Since(session.touched) > archiveUploadTTL {
			stale = append(stale, key)
			continue
		}
		if session.owner == owner {
			open++
			if !session.busy && (oldest == "" || session.touched.Before(u.sessions[oldest].touched)) {
				oldest = key
			}
		}
	}
	if open >= archiveUploadsOpen && oldest != "" {
		stale = append(stale, oldest)
	}
	u.sessions[id] = &archiveUploadSession{owner: owner, name: name, path: file.Name(), size: size, touched: time.Now()}
	u.mu.Unlock()
	for _, key := range stale {
		u.remove(key)
	}
	return id, nil
}

func (s *Server) archiveUploadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/connectors/archive/uploads", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		var in struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Name == "" || len(in.Name) > 255 || in.Size < 1 {
			s.fail(w, memory.ErrInvalid)
			return
		}
		if in.Size > connectors.MaxUploadBytes {
			s.archiveFail(w, r, &connectors.ArchiveError{Code: "archive_too_large"})
			return
		}
		id, err := uploads.open(scope.OwnerID, in.Name, in.Size)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "pieceBytes": archivePieceBytes})
	}))
	// Pieces arrive in order. A piece at any other offset is answered with how
	// much has arrived, so a sender that lost a reply can carry on from there.
	mux.HandleFunc("PUT /v1/connectors/archive/uploads/{id}", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		if err != nil || offset < 0 {
			s.fail(w, memory.ErrInvalid)
			return
		}
		session, err := uploads.take(scope.OwnerID, r.PathValue("id"))
		if err != nil {
			s.fail(w, err)
			return
		}
		defer uploads.release(session)
		if offset != session.received {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "upload_offset", "received": session.received})
			return
		}
		file, err := os.OpenFile(session.path, os.O_WRONLY, 0o600)
		if err != nil {
			s.fail(w, err)
			return
		}
		defer file.Close()
		if _, err = file.Seek(session.received, io.SeekStart); err != nil {
			s.fail(w, err)
			return
		}
		room := min(session.size-session.received, 4*archivePieceBytes)
		n, err := io.Copy(file, io.LimitReader(r.Body, room+1))
		if err != nil || n == 0 || n > room {
			// Nothing past the confirmed length may stay: the file is what gets imported.
			_ = file.Truncate(session.received)
			s.fail(w, memory.ErrInvalid)
			return
		}
		session.received += n
		writeJSON(w, http.StatusOK, map[string]any{"received": session.received})
	}))
	mux.HandleFunc("DELETE /v1/connectors/archive/uploads/{id}", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		session, err := uploads.take(scope.OwnerID, r.PathValue("id"))
		if err != nil && !errors.Is(err, memory.ErrNotFound) {
			s.fail(w, err)
			return
		}
		if session != nil {
			uploads.remove(r.PathValue("id"))
		}
		writeJSON(w, http.StatusOK, map[string]bool{"discarded": true})
	}))
}

// archiveInput reads the archive either from the request itself or from
// pieces sent earlier. used is called once an import has taken the archive.
func (s *Server) archiveInput(w http.ResponseWriter, r *http.Request, scope memory.Scope, preview bool) (io.ReadCloser, string, string, func(), bool) {
	validOrganize := func(organize string) bool {
		return preview || organize == "" || organize == "later" || organize == "now"
	}
	if media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); media == "application/json" {
		var in struct {
			Upload   string `json:"upload"`
			Organize string `json:"organize"`
		}
		if !decode(w, r, &in) {
			return nil, "", "", nil, false
		}
		if !validOrganize(in.Organize) {
			s.fail(w, memory.ErrInvalid)
			return nil, "", "", nil, false
		}
		session, err := uploads.take(scope.OwnerID, in.Upload)
		if err != nil {
			s.fail(w, err)
			return nil, "", "", nil, false
		}
		if session.received != session.size {
			uploads.release(session)
			s.fail(w, memory.ErrInvalid)
			return nil, "", "", nil, false
		}
		file, err := os.Open(session.path)
		if err != nil {
			uploads.release(session)
			s.fail(w, err)
			return nil, "", "", nil, false
		}
		taken := false
		return &sessionFile{File: file, done: func() {
			uploads.release(session)
			if taken {
				uploads.remove(in.Upload)
			}
		}}, session.name, in.Organize, func() { taken = true }, true
	}
	// Multipart overhead is separate from the file size checked by OpenArchive.
	r.Body = http.MaxBytesReader(w, r.Body, connectors.MaxUploadBytes+(1<<20))
	err := r.ParseMultipartForm(1 << 20)
	cleanup := func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}
	if err != nil {
		cleanup()
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			s.archiveFail(w, r, &connectors.ArchiveError{Code: "archive_too_large"})
		} else {
			s.fail(w, memory.ErrInvalid)
		}
		return nil, "", "", nil, false
	}
	organize := r.FormValue("organize")
	if !validOrganize(organize) {
		cleanup()
		s.fail(w, memory.ErrInvalid)
		return nil, "", "", nil, false
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		cleanup()
		s.fail(w, memory.ErrInvalid)
		return nil, "", "", nil, false
	}
	if header.Size > connectors.MaxUploadBytes {
		_ = file.Close()
		cleanup()
		s.archiveFail(w, r, &connectors.ArchiveError{Code: "archive_too_large"})
		return nil, "", "", nil, false
	}
	return &formFile{File: file, done: cleanup}, header.Filename, organize, func() {}, true
}

type sessionFile struct {
	*os.File
	done func()
}

func (f *sessionFile) Close() error { err := f.File.Close(); f.done(); return err }

type formFile struct {
	multipart.File
	done func()
}

func (f *formFile) Close() error { err := f.File.Close(); f.done(); return err }
