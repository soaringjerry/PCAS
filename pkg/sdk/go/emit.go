package pcas

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

// EmitOptions provides optional parameters for emitting events
type EmitOptions struct {
	Source  string // Event source (defaults to "pcas-sdk")
	Subject string // Optional event subject
	TraceID string // Optional trace ID (auto-generated if not provided)
}

// Emit sends an event to the PCAS event bus
func (c *Client) Emit(ctx context.Context, eventType string, data map[string]interface{}) error {
	return c.EmitWithOptions(ctx, eventType, data, EmitOptions{})
}

// EmitWithOptions sends an event with additional options
func (c *Client) EmitWithOptions(ctx context.Context, eventType string, data map[string]interface{}, opts EmitOptions) error {
	// Set defaults
	if opts.Source == "" {
		opts.Source = "pcas-sdk"
	}
	if opts.TraceID == "" {
		opts.TraceID = uuid.New().String()
	}

	// Create the event
	event := &eventsv1.Event{
		Id:          uuid.New().String(),
		Type:        eventType,
		Source:      opts.Source,
		Specversion: "1.0",
		Time:        timestamppb.New(time.Now()),
		TraceId:     opts.TraceID,
		Subject:     opts.Subject,
	}

	// Convert data to protobuf Any if provided
	if data != nil && len(data) > 0 {
		// Convert to structpb.Value
		value, err := structpb.NewValue(data)
		if err != nil {
			return fmt.Errorf("failed to convert data to protobuf: %w", err)
		}

		// Wrap in Any
		anyData, err := anypb.New(value)
		if err != nil {
			return fmt.Errorf("failed to wrap data in Any: %w", err)
		}

		event.Data = anyData
	}

	// Publish the event
	_, err := c.grpcClient.Publish(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	return nil
}