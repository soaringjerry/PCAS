package workspace

import (
	"context"
	"errors"
	"time"
)

var ErrTimezone = errors.New("invalid timezone")

// ValidateTimezone rejects implicit process-local zones as well as unknown names.
func ValidateTimezone(zone string) error {
	if zone == "" || zone == "Local" {
		return ErrTimezone
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return ErrTimezone
	}
	return nil
}

type initialTimezoneKey struct{}

// WithInitialTimezone supplies a browser preference for workspace creation only.
func WithInitialTimezone(ctx context.Context, zone string) (context.Context, error) {
	if err := ValidateTimezone(zone); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, initialTimezoneKey{}, zone), nil
}

func InitialTimezone(ctx context.Context) string {
	if zone, ok := ctx.Value(initialTimezoneKey{}).(string); ok {
		return zone
	}
	// Non-browser clients have no local preference to send.
	return "UTC"
}
