package notify

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// TelegramReceipt is a delivery association, never a copy of a desk response.
// Bot/chat are the confirmed active identity in Credentials.
type TelegramReceipt struct {
	MessageID      int64  `json:"messageId"`
	RequestID      string `json:"requestId"`
	ConversationID string `json:"conversationId"`
	TurnID         string `json:"turnId"`
	SentAt         int64  `json:"sentAt"`
}

type Credentials struct {
	VAPIDPublic                string            `json:"vapidPublic"`
	VAPIDPrivate               string            `json:"vapidPrivate"`
	TelegramToken              string            `json:"telegramToken"`
	TelegramChatID             string            `json:"telegramChatId"`
	TelegramOffset             int64             `json:"telegramOffset,omitempty"`
	TelegramConversation       string            `json:"telegramConversation,omitempty"`
	TelegramBotID              string            `json:"telegramBotId,omitempty"`
	TelegramTokenHash          string            `json:"telegramTokenHash,omitempty"`
	TelegramLegacyConversation string            `json:"telegramLegacyConversation,omitempty"`
	TelegramReceipts           []TelegramReceipt `json:"telegramReceipts,omitempty"`
}

type Settings struct{ Path string }

var settingsLocks sync.Map

func SettingsPath() string {
	if p := os.Getenv("PCAS_NOTIFY_SETTINGS_FILE"); p != "" {
		return p
	}
	p := os.Getenv("PCAS_MODEL_SETTINGS_FILE")
	if p == "" {
		p = "data/models.json"
	}
	return filepath.Join(filepath.Dir(p), "notify.json")
}
func (s Settings) lock() *sync.Mutex {
	p, _ := filepath.Abs(s.Path)
	value, _ := settingsLocks.LoadOrStore(p, &sync.Mutex{})
	return value.(*sync.Mutex)
}
func (s Settings) read() (Credentials, error) {
	var c Credentials
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return c, err
	}
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("invalid notification settings")
	}
	return c, nil
}
func (s Settings) Read() (Credentials, error) {
	mu := s.lock()
	mu.Lock()
	defer mu.Unlock()
	return s.read()
}
func (s Settings) update(change func(*Credentials) error) (Credentials, error) {
	mu := s.lock()
	mu.Lock()
	defer mu.Unlock()
	c, err := s.read()
	if err != nil {
		return c, err
	}
	if err = change(&c); err != nil {
		return c, err
	}
	if err = os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return c, err
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".notify-*")
	if err != nil {
		return c, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return c, err
	}
	if err = json.NewEncoder(f).Encode(c); err != nil {
		return c, err
	}
	if err = f.Sync(); err != nil {
		return c, err
	}
	if err = f.Close(); err != nil {
		return c, err
	}
	return c, os.Rename(f.Name(), s.Path)
}
func (s Settings) EnsureVAPID() (Credentials, error) {
	return s.update(func(c *Credentials) error {
		if c.VAPIDPublic != "" && c.VAPIDPrivate != "" {
			return nil
		}
		var err error
		c.VAPIDPrivate, c.VAPIDPublic, err = webpush.GenerateVAPIDKeys()
		return err
	})
}
func (s Settings) SaveTelegram(token, chatID string) error {
	_, err := s.update(func(c *Credentials) error {
		if c.TelegramToken != token || c.TelegramChatID != chatID {
			c.TelegramOffset, c.TelegramConversation = 0, ""
			c.TelegramTokenHash, c.TelegramLegacyConversation = "", ""
			if token == "" || c.TelegramChatID != chatID {
				c.TelegramBotID, c.TelegramReceipts = "", nil
			}
		}
		c.TelegramToken, c.TelegramChatID = token, chatID
		return nil
	})
	return err
}

// UpdateTelegramProgress shares the credential writer's lock and atomic replace.
// A stopped poller cannot overwrite progress for a newly configured bot/chat.
func (s Settings) UpdateTelegramProgress(token, chatID string, offset int64, conversation string) error {
	_, err := s.update(func(c *Credentials) error {
		if token == "" || chatID == "" || c.TelegramToken != token || c.TelegramChatID != chatID {
			return errors.New("Telegram configuration changed")
		}
		if offset > c.TelegramOffset {
			c.TelegramOffset = offset
		}
		c.TelegramConversation = conversation
		return nil
	})
	return err
}

// TelegramCredentialHash fences an identity resolved for a particular token.
// It is never the bot identity used to derive business request IDs.
func TelegramCredentialHash(token string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
}

func telegramConfigured(c *Credentials, token, chatID string) bool {
	return token != "" && chatID != "" && c.TelegramToken == token && c.TelegramChatID == chatID
}

// ConfirmTelegramIdentity follows getMe. Preserve associations across credential
// rotation for the same bot, but never lend them to a different bot or chat.
func (s Settings) ConfirmTelegramIdentity(token, chatID, botID string) (Credentials, error) {
	return s.update(func(c *Credentials) error {
		if !telegramConfigured(c, token, chatID) || botID == "" {
			return errors.New("Telegram configuration changed")
		}
		if c.TelegramBotID == "" {
			c.TelegramLegacyConversation = c.TelegramConversation
		} else if c.TelegramBotID != botID {
			c.TelegramReceipts = nil
			c.TelegramLegacyConversation = ""
		}
		c.TelegramBotID = botID
		c.TelegramTokenHash = TelegramCredentialHash(token)
		pruneTelegramReceipts(c, time.Now())
		return nil
	})
}

func pruneTelegramReceipts(c *Credentials, now time.Time) {
	cutoff := now.Add(-30 * 24 * time.Hour).Unix()
	kept := make([]TelegramReceipt, 0, len(c.TelegramReceipts))
	for _, r := range c.TelegramReceipts {
		if r.SentAt > cutoff {
			kept = append(kept, r)
		}
	}
	c.TelegramReceipts = kept
}

// RecordTelegramReceipt is called only after sendMessage returned a real ID.
// A canceled session cannot attach its delivery to newly configured credentials.
func (s Settings) RecordTelegramReceipt(token, chatID, botID string, receipt TelegramReceipt) error {
	_, err := s.update(func(c *Credentials) error {
		if !telegramConfigured(c, token, chatID) || c.TelegramBotID != botID || c.TelegramTokenHash != TelegramCredentialHash(token) {
			return errors.New("Telegram configuration changed")
		}
		if receipt.MessageID <= 0 || receipt.RequestID == "" || receipt.ConversationID == "" || receipt.TurnID == "" {
			return errors.New("invalid Telegram receipt association")
		}
		pruneTelegramReceipts(c, time.Now())
		receipt.SentAt = time.Now().Unix()
		for i, old := range c.TelegramReceipts {
			if old.MessageID == receipt.MessageID {
				c.TelegramReceipts[i] = receipt
				return nil
			}
		}
		c.TelegramReceipts = append(c.TelegramReceipts, receipt)
		return nil
	})
	return err
}
