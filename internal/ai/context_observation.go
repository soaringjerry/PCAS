package ai

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func (r *Registry) generationContext(ctx context.Context, p Provider) context.Context {
	bound := memory.ContextRequestObserverFrom(ctx)
	if bound == nil && r.ContextObserver == nil {
		return ctx
	}
	return memory.WithContextRequestObserver(ctx, memory.ContextRequestObserverFunc(func(ctx context.Context, event memory.ContextRequestEvent) error {
		if event.ProviderID == "" {
			event.ProviderID = p.ID
		}
		if event.Protocol == "" {
			event.Protocol = p.Protocol
		}
		if event.Model == "" {
			event.Model = p.Model
		}
		if event.Endpoint == "" {
			event.Endpoint = p.BaseURL
		}
		// Prepared persistence precedes an observable barrier. Every later fence
		// runs after the observer returns, immediately before adapter dispatch.
		if event.Stage == "prepared" && bound != nil {
			if err := bound.ObserveContextRequest(ctx, event); err != nil {
				return err
			}
		}
		if r.ContextObserver != nil {
			if err := r.ContextObserver.ObserveContextRequest(ctx, event); err != nil {
				return err
			}
		}
		if event.Stage != "prepared" && bound != nil {
			return bound.ObserveContextRequest(ctx, event)
		}
		return nil
	}))
}
