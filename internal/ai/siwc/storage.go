// Package siwc implements the public-client ChatGPT plan authorization flow.
// Credentials are separate from PCAS sessions, API keys and Codex credentials.
package siwc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const UsageURL = "https://chatgpt.com/settings/usage"

type Account struct {
	ClientID          string    `json:"client_id"`
	Subject           string    `json:"subject"`
	Issuer            string    `json:"issuer"`
	Email             string    `json:"email,omitempty"`
	Scopes            []string  `json:"scopes"`
	AccessToken       string    `json:"access_token,omitempty"`
	RefreshToken      string    `json:"refresh_token,omitempty"`
	IDToken           string    `json:"id_token,omitempty"`
	ExpiresAt         time.Time `json:"expires_at"`
	EarliestRefreshAt int64     `json:"earliest_refresh_at,omitempty"`
	Session           string    `json:"session,omitempty"`
	Model             string    `json:"model,omitempty"`
	// ModelManual: the owner typed Model in; it is used without the catalog,
	// which can omit models the account is nevertheless able to run.
	ModelManual           bool `json:"model_manual,omitempty"`
	Paused                bool `json:"paused,omitempty"`
	Generated             bool `json:"generated,omitempty"`
	Refreshed             bool `json:"refreshed,omitempty"`
	GeneratedAfterRefresh bool `json:"generated_after_refresh,omitempty"`
	Verified              bool `json:"verified,omitempty"`
	Default               bool `json:"default_initialized,omitempty"`
}

func (a Account) PlanEnabled() bool {
	return a.AccessToken != "" && hasScope(a.Scopes, "chatgpt.tokens.use.direct") && hasScope(a.Scopes, "resource.invoke")
}
func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}
func (a *Account) clearTokens() {
	a.AccessToken, a.RefreshToken, a.IDToken, a.Session = "", "", "", ""
	a.ExpiresAt = time.Time{}
	a.Paused = false
}

type diskState struct {
	HostID   string    `json:"ext_agent_host_id"`
	Active   string    `json:"active_client_id,omitempty"`
	Accounts []Account `json:"accounts"`
}

func (d *diskState) account(id string) *Account {
	for i := range d.Accounts {
		if d.Accounts[i].ClientID == id {
			return &d.Accounts[i]
		}
	}
	return nil
}

// flock serializes credential rotation across API, worker and local helpers.
// The lock lives beside the atomically replaced credential file, not on it.
func (m *Manager) locked(ctx context.Context, fn func(*diskState) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(m.dir, "session.lock"), os.O_CREATE|os.O_RDWR|openNoFollow, 0600)
	if err != nil {
		return fmt.Errorf("cannot lock ChatGPT credentials")
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	for {
		held, err := tryLock(f)
		if err != nil {
			return fmt.Errorf("cannot lock ChatGPT credentials")
		}
		if !held {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	defer unlock(f)
	if err := ctx.Err(); err != nil {
		return err
	}
	d, err := m.read()
	if err != nil {
		return err
	}
	return fn(&d)
}
func (m *Manager) read() (diskState, error) {
	var d diskState
	f, err := os.OpenFile(filepath.Join(m.dir, "credentials.json"), os.O_RDONLY|openNoFollow, 0)
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return d, fmt.Errorf("cannot read ChatGPT credentials")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !ownerOnly(info) || info.Size() > 1<<20 {
		return d, fmt.Errorf("ChatGPT credentials must be a protected regular file")
	}
	if json.NewDecoder(f).Decode(&d) != nil {
		return d, fmt.Errorf("invalid ChatGPT credential file")
	}
	return d, nil
}
func (m *Manager) save(d *diskState) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.dir, ".credentials-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("cannot save ChatGPT credentials")
	}
	if err = os.Rename(name, filepath.Join(m.dir, "credentials.json")); err != nil {
		return err
	}
	return syncDir(m.dir)
}
