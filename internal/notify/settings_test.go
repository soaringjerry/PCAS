package notify

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSettingsAtomicPrivateAndStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "notify.json")
	settings := Settings{Path: path}
	first, err := settings.EnsureVAPID()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Independent Settings values exercise the shared per-path write lock.
			s := Settings{Path: path}
			if err := s.SaveTelegram("synthetic-"+filepath.Base(t.TempDir()), "123"); err != nil {
				t.Error(err)
			}
			got, err := s.EnsureVAPID()
			if err != nil {
				t.Error(err)
				return
			}
			if got.VAPIDPublic != first.VAPIDPublic || got.VAPIDPrivate != first.VAPIDPrivate {
				t.Error("VAPID keys changed")
			}
		}()
	}
	wg.Wait()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	got, err := settings.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.TelegramChatID != "123" || got.VAPIDPublic != first.VAPIDPublic {
		t.Fatal("lost a concurrent update")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("temporary settings file leaked")
	}
}
func TestSettingsPath(t *testing.T) {
	t.Setenv("PCAS_NOTIFY_SETTINGS_FILE", "")
	t.Setenv("PCAS_MODEL_SETTINGS_FILE", "/private/model.json")
	if SettingsPath() != "/private/notify.json" {
		t.Fatal("not beside model settings")
	}
	t.Setenv("PCAS_NOTIFY_SETTINGS_FILE", "/override/notify.json")
	if SettingsPath() != "/override/notify.json" {
		t.Fatal("override ignored")
	}
}
func TestDamagedSettingsDoNotReplaceKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notify.json")
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Settings{Path: path}).EnsureVAPID(); err == nil {
		t.Fatal("damaged settings silently replaced")
	}
}
