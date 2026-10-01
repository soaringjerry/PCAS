package workspace

import (
	"context"
	"errors"
	"testing"
)

func TestTimezoneValidation(t *testing.T) {
	for _, zone := range []string{"Australia/Melbourne", "Asia/Shanghai", "UTC", "America/New_York"} {
		ctx, err := WithInitialTimezone(context.Background(), zone)
		if err != nil || InitialTimezone(ctx) != zone {
			t.Fatalf("%q: %v", zone, err)
		}
	}
	for _, zone := range []string{"", "Local", "Melbourne", "Australia/Unknown", "../etc/passwd", " Asia/Shanghai "} {
		if err := ValidateTimezone(zone); !errors.Is(err, ErrTimezone) {
			t.Fatalf("accepted %q: %v", zone, err)
		}
	}
	if InitialTimezone(context.Background()) != "UTC" {
		t.Fatal("non-browser default must be explicit UTC")
	}
}
