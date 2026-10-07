package ai

import "context"

type invocationObserverKey struct{}

// WithInvocationObserver observes the measured channel call without changing
// the request, result, deadline or retry policy. Callers must be concurrency safe.
func WithInvocationObserver(ctx context.Context, observe func(int64)) context.Context {
	return context.WithValue(ctx, invocationObserverKey{}, observe)
}

func observeInvocation(ctx context.Context, ms int64) {
	if observe, ok := ctx.Value(invocationObserverKey{}).(func(int64)); ok {
		observe(ms)
	}
}
