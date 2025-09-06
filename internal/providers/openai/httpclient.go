package openai

import (
    "net/http"
)

// makeHTTPDoerWithHeaders returns an *http.Client that injects headers into every request.
func makeHTTPDoerWithHeaders(headers map[string]string) *http.Client {
    return &http.Client{
        Transport: &headerRoundTripper{
            next:    http.DefaultTransport,
            headers: headers,
        },
    }
}

type headerRoundTripper struct {
    next    http.RoundTripper
    headers map[string]string
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
    for k, v := range h.headers {
        if v != "" {
            req.Header.Set(k, v)
        }
    }
    return h.next.RoundTrip(req)
}

