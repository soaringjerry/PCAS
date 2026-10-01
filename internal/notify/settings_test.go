package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

func TestTelegramProgressSurvivesCredentialWrites(t *testing.T) {
	s := Settings{Path: filepath.Join(t.TempDir(), "notify.json")}
	first, err := s.EnsureVAPID()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTelegram("fixture", "123"); err != nil {
		t.Fatal(err)
	}
	conversation := "11111111-1111-4111-8111-111111111111"
	if err := s.UpdateTelegramProgress("fixture", "123", 40, conversation); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTelegram("fixture", "123"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureVAPID(); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTelegramProgress("fixture", "123", 12, conversation); err != nil {
		t.Fatal(err)
	}
	c, err := (Settings{Path: s.Path}).Read()
	if err != nil || c.TelegramOffset != 40 || c.TelegramConversation != conversation || c.VAPIDPublic != first.VAPIDPublic || c.VAPIDPrivate != first.VAPIDPrivate {
		t.Fatal("progress or keys lost", err)
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["telegramOffset"]) != "40" || string(raw["telegramConversation"]) != `"`+conversation+`"` {
		t.Fatal("incorrect JSON fields")
	}
	if err := s.SaveTelegram("next-fixture", "456"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTelegramProgress("fixture", "123", 100, conversation); err == nil {
		t.Fatal("old poller overwrote new credentials")
	}
	c, err = s.Read()
	if err != nil || c.TelegramOffset != 0 || c.TelegramConversation != "" || c.VAPIDPublic != first.VAPIDPublic {
		t.Fatal("new chat inherited progress", err)
	}
}

func TestTelegramProgressConcurrentUpdates(t *testing.T) {
	s := Settings{Path: filepath.Join(t.TempDir(), "notify.json")}
	if err := s.SaveTelegram("fixture", "123"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 1; i <= 32; i++ {
		wg.Go(func() {
			if err := (Settings{Path: s.Path}).UpdateTelegramProgress("fixture", "123", int64(i), "conversation"); err != nil {
				t.Error(err)
			}
			if _, err := (Settings{Path: s.Path}).EnsureVAPID(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	c, err := s.Read()
	if err != nil || c.TelegramOffset != 32 || c.TelegramConversation != "conversation" || c.VAPIDPublic == "" {
		t.Fatal("concurrent progress lost", err)
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

func TestTelegramReceiptIdentityGuardAndThirtyDayRetention(t *testing.T) {
	s := Settings{Path: filepath.Join(t.TempDir(), "notify.json")}
	if err := s.SaveTelegram("old-fixture", "123"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmTelegramIdentity("old-fixture", "123", "7001"); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 400; i++ {
		r := TelegramReceipt{MessageID: i, RequestID: "00000000-0000-4000-8000-000000000001", ConversationID: "00000000-0000-4000-8000-000000000002", TurnID: "00000000-0000-4000-8000-000000000003"}
		if err := s.RecordTelegramReceipt("old-fixture", "123", "7001", r); err != nil {
			t.Fatal(err)
		}
	}
	c, err := s.Read()
	if err != nil || len(c.TelegramReceipts) != 400 {
		t.Fatal("retained bindings were capped or unreadable", len(c.TelegramReceipts), err)
	}
	data, err := os.ReadFile(s.Path)
	if err != nil || len(data) <= 64<<10 {
		t.Fatal("fixture did not exercise association file above old read limit", len(data), err)
	}
	if err := s.SaveTelegram("rotated-fixture", "123"); err != nil {
		t.Fatal(err)
	}
	r := c.TelegramReceipts[0]
	if err := s.RecordTelegramReceipt("old-fixture", "123", "7001", r); err == nil {
		t.Fatal("old credential attached a receipt")
	}
	if _, err := s.ConfirmTelegramIdentity("old-fixture", "123", "7001"); err == nil {
		t.Fatal("old credential confirmed an identity")
	}
	if err := s.RecordTelegramReceipt("rotated-fixture", "123", "7001", r); err == nil {
		t.Fatal("unconfirmed rotation attached receipt")
	}
	if _, err := s.ConfirmTelegramIdentity("rotated-fixture", "123", "7001"); err != nil {
		t.Fatal(err)
	}
	c, _ = s.Read()
	if len(c.TelegramReceipts) != 400 {
		t.Fatal("same bot rotation discarded live bindings")
	}
	_, err = s.update(func(c *Credentials) error {
		c.TelegramReceipts[0].SentAt = time.Now().Add(-31 * 24 * time.Hour).Unix()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.ConfirmTelegramIdentity("rotated-fixture", "123", "7001")
	if err != nil || len(c.TelegramReceipts) != 399 {
		t.Fatal("expired bindings not pruned", err)
	}
	if err := s.SaveTelegram("second-fixture", "123"); err != nil {
		t.Fatal(err)
	}
	c, err = s.ConfirmTelegramIdentity("second-fixture", "123", "7002")
	if err != nil || len(c.TelegramReceipts) != 0 || c.TelegramLegacyConversation != "" {
		t.Fatal("new bot borrowed old bindings", err)
	}
	if err := s.SaveTelegram("", ""); err != nil {
		t.Fatal(err)
	}
	c, _ = s.Read()
	if c.TelegramBotID != "" || c.TelegramTokenHash != "" || len(c.TelegramReceipts) != 0 {
		t.Fatal("removed bot kept association identity")
	}
}
