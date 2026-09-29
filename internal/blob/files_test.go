package blob

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestFilesIsolationAndIndependentUploads(t *testing.T) {
	f, err := NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	scope := memory.Scope{OwnerID: memory.NewID(), PrincipalID: "owner", IsOwner: true}
	first, err := f.Put(ctx, scope, strings.NewReader("original"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.Put(ctx, scope, strings.NewReader("original"))
	if err != nil || first == second {
		t.Fatal("uploads must have independent cleanup identities", err)
	}
	if _, err := f.Open(ctx, memory.Scope{OwnerID: memory.NewID(), PrincipalID: "owner", IsOwner: true}, first); err == nil {
		t.Fatal("cross-owner file read")
	}
	if _, err := f.Open(ctx, scope, string(scope.OwnerID)+"/../../etc/passwd"); err == nil {
		t.Fatal("unsafe path")
	}
	if err := f.Delete(ctx, scope, first); err != nil {
		t.Fatal(err)
	}
	file, err := f.Open(ctx, scope, second)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || string(data) != "original" {
		t.Fatal("cleanup touched another upload", err)
	}
}
