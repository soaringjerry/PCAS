package workspace

import (
	"context"
	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type Notice struct {
	ID          string `json:"id"`
	ThingID     string `json:"thingId"`
	Title       string `json:"title"`
	Reason      string `json:"reason"`
	DueAt       string `json:"dueAt"`
	CreatedAt   string `json:"createdAt"`
	DismissedAt string `json:"dismissedAt,omitempty"`
}
type NotifyConfig struct {
	WebPush struct {
		PublicKey     string `json:"publicKey"`
		Subscriptions int    `json:"subscriptions"`
	} `json:"webPush"`
	Telegram struct {
		Configured bool   `json:"configured"`
		ChatID     string `json:"chatId"`
	} `json:"telegram"`
}
type TelegramConfig struct {
	BotToken string `json:"botToken"`
	ChatID   string `json:"chatId"`
}

// NotifyAPI is supplied through httpapi.Options.Workspace, just like other
// optional workspace capabilities, without exposing transport credentials.
type NotifyAPI interface {
	NotifyConfig(context.Context, memory.Scope) (NotifyConfig, error)
	SavePushSubscription(context.Context, memory.Scope, webpush.Subscription) error
	RemovePushSubscription(context.Context, memory.Scope, string) error
	SaveTelegram(context.Context, memory.Scope, TelegramConfig) (bool, error)
	TestNotify(context.Context, memory.Scope) ([]string, error)
	DismissNotice(context.Context, memory.Scope, string) (State, error)
}
