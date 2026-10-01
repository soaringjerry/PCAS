package memory

import "context"

// ContextRequestEvent observes the last PCAS-controlled request assembly. The
// payload is transient and never written to ordinary logs. A dispatched event
// observes a send attempt, not confirmed receipt or a provider's hidden input.
type ContextRequestEvent struct {
	Stage            string
	ProviderID       string
	Protocol         string
	Model            string
	Endpoint         string
	Payload          []byte
	ObservationLayer string
}

type ContextRequestObserver interface {
	ObserveContextRequest(context.Context, ContextRequestEvent) error
}
type ContextRequestObserverFunc func(context.Context, ContextRequestEvent) error

func (f ContextRequestObserverFunc) ObserveContextRequest(ctx context.Context, event ContextRequestEvent) error {
	return f(ctx, event)
}

type contextRequestObserverKey struct{}

func WithContextRequestObserver(ctx context.Context, observer ContextRequestObserver) context.Context {
	return context.WithValue(ctx, contextRequestObserverKey{}, observer)
}
func ContextRequestObserverFrom(ctx context.Context) ContextRequestObserver {
	observer, _ := ctx.Value(contextRequestObserverKey{}).(ContextRequestObserver)
	return observer
}
func ObserveContextRequest(ctx context.Context, event ContextRequestEvent) error {
	if observer := ContextRequestObserverFrom(ctx); observer != nil {
		return observer.ObserveContextRequest(ctx, event)
	}
	return nil
}
