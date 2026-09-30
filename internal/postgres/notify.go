package postgres

import (
	"context"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Notifier extends the workspace supplied in Options without modifying its
// existing API. Transport dependencies are injectable for isolated tests.
type Notifier struct {
	*Store
	Settings  notify.Settings
	Telegram  *notify.Telegram
	Channels  []notify.Channel
	PublicURL string
}

func NewNotifier(s *Store, settings notify.Settings, publicURL string) *Notifier {
	telegram := &notify.Telegram{Settings: settings}
	return &Notifier{Store: s, Settings: settings, Telegram: telegram, PublicURL: strings.TrimRight(publicURL, "/"), Channels: []notify.Channel{&notify.WebPush{Settings: settings, Store: s, PublicURL: publicURL}, telegram}}
}

var _ workspace.NotifyAPI = (*Notifier)(nil)

func (s *Store) Subscriptions(ctx context.Context, owner string) ([]webpush.Subscription, error) {
	rows, err := s.pool.Query(ctx, "SELECT endpoint,p256dh,auth FROM push_subscriptions WHERE owner_id=$1 ORDER BY created_at,endpoint", owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []webpush.Subscription{}
	for rows.Next() {
		var sub webpush.Subscription
		if err := rows.Scan(&sub.Endpoint, &sub.Keys.P256dh, &sub.Keys.Auth); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
func (s *Store) DeleteSubscription(ctx context.Context, owner, endpoint string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM push_subscriptions WHERE owner_id=$1 AND endpoint=$2", owner, endpoint)
	return err
}
func (n *Notifier) NotifyConfig(ctx context.Context, scope memory.Scope) (workspace.NotifyConfig, error) {
	var out workspace.NotifyConfig
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	keys, err := n.Settings.EnsureVAPID()
	if err != nil {
		return out, err
	}
	out.WebPush.PublicKey = keys.VAPIDPublic
	if err = n.pool.QueryRow(ctx, "SELECT count(*) FROM push_subscriptions WHERE owner_id=$1", string(scope.OwnerID)).Scan(&out.WebPush.Subscriptions); err != nil {
		return out, err
	}
	out.Telegram.Configured = keys.TelegramToken != "" && keys.TelegramChatID != ""
	out.Telegram.ChatID = keys.TelegramChatID
	return out, nil
}
func (n *Notifier) SavePushSubscription(ctx context.Context, scope memory.Scope, sub webpush.Subscription) error {
	if err := requireOwner(scope); err != nil {
		return err
	}
	u, err := url.Parse(sub.Endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(sub.Endpoint) > 4096 {
		return memory.ErrInvalid
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		return memory.ErrInvalid
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return memory.ErrInvalid
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(sub.Keys.P256dh, "="))
	if err != nil {
		return memory.ErrInvalid
	}
	x, _ := elliptic.Unmarshal(elliptic.P256(), key)
	if x == nil {
		return memory.ErrInvalid
	}
	auth, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(sub.Keys.Auth, "="))
	if err != nil || len(auth) != 16 {
		return memory.ErrInvalid
	}
	return pgx.BeginFunc(ctx, n.pool, func(tx pgx.Tx) error {
		if err := n.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO push_subscriptions(owner_id,endpoint,p256dh,auth) VALUES($1,$2,$3,$4)
  ON CONFLICT(owner_id,endpoint) DO UPDATE SET p256dh=excluded.p256dh,auth=excluded.auth`, string(scope.OwnerID), sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth)
		return err
	})
}
func (n *Notifier) RemovePushSubscription(ctx context.Context, scope memory.Scope, endpoint string) error {
	if err := requireOwner(scope); err != nil {
		return err
	}
	if endpoint == "" {
		return memory.ErrInvalid
	}
	return n.DeleteSubscription(ctx, string(scope.OwnerID), endpoint)
}

var telegramTokenPattern = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)

func (n *Notifier) SaveTelegram(ctx context.Context, scope memory.Scope, in workspace.TelegramConfig) (bool, error) {
	if err := requireOwner(scope); err != nil {
		return false, err
	}
	token, chatID := strings.TrimSpace(in.BotToken), strings.TrimSpace(in.ChatID)
	if token == "" {
		return false, n.Settings.SaveTelegram("", "")
	}
	// Keep credential-bearing URLs well-formed; transport errors stay redacted.
	if len(token) > 256 || !telegramTokenPattern.MatchString(token) {
		return false, notify.ErrTelegramTokenInvalid
	}
	if chatID == "" {
		var err error
		chatID, err = n.Telegram.ResolveChat(ctx, token)
		if err != nil {
			if errors.Is(err, notify.ErrTelegramTokenInvalid) || errors.Is(err, notify.ErrTelegramWebhookActive) || errors.Is(err, notify.ErrTelegramNoChat) {
				return false, err
			}
			return false, notify.ErrTelegramSendFailed
		}
	}
	if _, err := strconv.ParseInt(chatID, 10, 64); err != nil {
		return false, notify.ErrTelegramSendFailed
	}
	if err := n.Telegram.SendTo(ctx, token, chatID, notify.Message{Title: "PCAS 提醒已连接", Body: time.Now().Format("2006-01-02 15:04 MST"), URL: n.PublicURL + "/settings"}); err != nil {
		if errors.Is(err, notify.ErrTelegramTokenInvalid) {
			return false, err
		}
		return false, notify.ErrTelegramSendFailed
	}
	if err := n.Settings.SaveTelegram(token, chatID); err != nil {
		return false, err
	}
	return true, nil
}
func (n *Notifier) TestNotify(ctx context.Context, scope memory.Scope) ([]string, error) {
	if err := requireOwner(scope); err != nil {
		return nil, err
	}
	state, err := n.Snapshot(ctx, scope)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(state.Settings.Timezone)
	if err != nil {
		return nil, err
	}
	m := notify.Message{OwnerID: string(scope.OwnerID), NoticeID: string(memory.NewID()), Title: "PCAS 测试提醒", Body: time.Now().In(loc).Format("2006-01-02 15:04 MST") + " · 通知通道测试", URL: n.PublicURL + "/settings"}
	sent := []string{}
	for _, channel := range n.Channels {
		if channel.Send(ctx, m) == nil {
			sent = append(sent, channel.Name())
		}
	}
	return sent, nil
}
func (n *Notifier) DismissNotice(ctx context.Context, scope memory.Scope, id string) (workspace.State, error) {
	var out workspace.State
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(id).Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginFunc(ctx, n.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE workspace_notices SET dismissed_at=COALESCE(dismissed_at,now()) WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return memory.ErrNotFound
		}
		if _, err := tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
			return err
		}
		out, err = n.snapshotTx(ctx, tx, scope)
		return err
	})
	return out, err
}

func getPublicURL() string { return os.Getenv("PCAS_PUBLIC_URL") }

func (s *Store) RunNotify(ctx context.Context, logger *slog.Logger, channels []notify.Channel) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if err := s.DispatchNotices(ctx, now, channels); err != nil && ctx.Err() == nil {
				logger.Warn("notification dispatch failed")
			}
		}
	}
}

type deliveryState struct {
	Attempts map[string]int       `json:"attempts"`
	RetryAt  map[string]time.Time `json:"retryAt"`
}

// DispatchNotices uses a transaction advisory lock per notice to keep concurrent
// dispatchers from sending it twice. READ COMMITTED rechecks state per channel.
func (s *Store) DispatchNotices(ctx context.Context, now time.Time, channels []notify.Channel) error {
	rows, err := s.pool.Query(ctx, `SELECT id::text FROM workspace_notices WHERE created_at>=$1 AND created_at<=$2 AND dismissed_at IS NULL ORDER BY created_at,id`, now.Add(-24*time.Hour), now)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			var locked bool
			if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))", "pcas-notice:"+id).Scan(&locked); err != nil {
				return err
			}
			if !locked {
				return nil
			}
			for _, channel := range channels {
				var owner, title, thing, reason, timezone string
				var due time.Time
				var raw []byte
				// Re-read immediately before every send, including dismissal and followUps.
				err := tx.QueryRow(ctx, `SELECT n.owner_id::text,n.thing_id::text,w.title,n.reason,n.due_at,o.settings->>'timezone',n.delivered
      FROM workspace_notices n JOIN work_items w ON (w.owner_id,w.id)=(n.owner_id,n.thing_id)
      JOIN workspace_owners o ON o.owner_id=n.owner_id
      WHERE n.id=$1 AND n.dismissed_at IS NULL AND w.status NOT IN ('done','cancelled','dropped','promoted')
      AND o.settings->>'followUps'='true'`, id).Scan(&owner, &thing, &title, &reason, &due, &timezone, &raw)
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				if err != nil {
					return err
				}
				var fields map[string]json.RawMessage
				var delivery deliveryState
				if err := json.Unmarshal(raw, &fields); err != nil {
					return err
				}
				if err := json.Unmarshal(raw, &delivery); err != nil {
					return err
				}
				name := channel.Name()
				if _, ok := fields[name]; ok {
					continue
				}
				attempts := delivery.Attempts[name]
				if attempts >= 5 || now.Before(delivery.RetryAt[name]) {
					continue
				}
				loc, err := time.LoadLocation(timezone)
				if err != nil {
					return err
				}
				publicURL := strings.TrimRight(getPublicURL(), "/")
				m := notify.Message{OwnerID: owner, NoticeID: id, ThingID: thing, Title: title, Body: due.In(loc).Format("2006-01-02 15:04 MST") + " · " + reason, URL: publicURL + "/t/" + thing}
				err = channel.Send(ctx, m)
				if errors.Is(err, notify.ErrUnconfigured) {
					continue
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				patch := map[string]any{}
				if err == nil {
					patch[name] = now.UTC().Format(time.RFC3339)
				} else {
					if delivery.Attempts == nil {
						delivery.Attempts = map[string]int{}
					}
					if delivery.RetryAt == nil {
						delivery.RetryAt = map[string]time.Time{}
					}
					attempts++
					if errors.Is(err, notify.ErrGone) {
						attempts = 5
					}
					delay := time.Minute
					if attempts == 2 {
						delay = 5 * time.Minute
					}
					if attempts >= 3 {
						delay = 30 * time.Minute
					}
					delivery.Attempts[name] = attempts
					delivery.RetryAt[name] = now.Add(delay)
					patch["attempts"] = delivery.Attempts
					patch["retryAt"] = delivery.RetryAt
				}
				if _, err := tx.Exec(ctx, "UPDATE workspace_notices SET delivered=delivered || $2::jsonb WHERE id=$1", id, asJSON(patch)); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
