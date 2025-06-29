// TODO: Update tests for unified storage interface
// +build ignore

package bus

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"github.com/soaringjerry/pcas/internal/storage"
)

// Mock types for testing
type MockStorage struct {
	mock.Mock
}

func (m *MockStorage) StoreEvent(ctx context.Context, event *eventsv1.Event, embedding []float32) error {
	args := m.Called(ctx, event, embedding)
	return args.Error(0)
}

func (m *MockStorage) GetEventByID(ctx context.Context, eventID string) (*eventsv1.Event, error) {
	args := m.Called(ctx, eventID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*eventsv1.Event), args.Error(1)
}

func (m *MockStorage) BatchGetEvents(ctx context.Context, ids []string) ([]*eventsv1.Event, error) {
	args := m.Called(ctx, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*eventsv1.Event), args.Error(1)
}

func (m *MockStorage) GetAllEvents(ctx context.Context, limit int, offset int) ([]*eventsv1.Event, error) {
	args := m.Called(ctx, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*eventsv1.Event), args.Error(1)
}

func (m *MockStorage) QuerySimilar(ctx context.Context, embedding []float32, topK int) ([]storage.QueryResult, error) {
	args := m.Called(ctx, embedding, topK)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.QueryResult), args.Error(1)
}

func (m *MockStorage) Close() error {
	args := m.Called()
	return args.Error(0)
}

