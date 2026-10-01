// Package contextwire observes entry into the real HTTP transport. It does not
// claim that an external service received or acknowledged the bytes.
package contextwire

import (
	"net/http"

	"github.com/soaringjerry/PCAS/internal/memory"
)

type transport struct {
	base  http.RoundTripper
	event memory.ContextRequestEvent
}

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	e := t.event
	e.Stage = "dispatched"
	if err := memory.ObserveContextRequest(req.Context(), e); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func Do(client *http.Client, req *http.Request, event memory.ContextRequestEvent) (*http.Response, error) {
	if memory.ContextRequestObserverFrom(req.Context()) == nil {
		return client.Do(req)
	}
	for _, stage := range []string{"prepared", "before_dispatch"} {
		event.Stage = stage
		if err := memory.ObserveContextRequest(req.Context(), event); err != nil {
			return nil, err
		}
	}
	copyClient := *client
	base := copyClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	copyClient.Transport = transport{base: base, event: event}
	// An automatic redirect changes the actual recipient. A fresh explicitly
	// authorized request is required rather than carrying the old raw policy.
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	return copyClient.Do(req)
}
