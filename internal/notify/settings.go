package notify

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type Credentials struct {
	VAPIDPublic    string `json:"vapidPublic"`
	VAPIDPrivate   string `json:"vapidPrivate"`
	TelegramToken  string `json:"telegramToken"`
	TelegramChatID string `json:"telegramChatId"`
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
	d := json.NewDecoder(io.LimitReader(f, 64<<10))
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
		c.TelegramToken, c.TelegramChatID = token, chatID
		return nil
	})
	return err
}
