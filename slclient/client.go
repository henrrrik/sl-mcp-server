package slclient

import (
	"net/http"
	"net/url"
	"time"
)

// HTTPDoer abstracts HTTP requests for testability.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// NewClient returns an HTTPDoer with sensible timeouts. The transport keeps
// more idle connections per host than the default (2): every tool call
// hits one of three SL hosts, so a burst of calls would otherwise tear
// down connections and pay a new TLS handshake each time.
func NewClient() HTTPDoer {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 16
	return &http.Client{Timeout: 15 * time.Second, Transport: transport}
}

// BuildURL constructs a full URL from base, path, and query parameters.
func BuildURL(base, path string, params url.Values) string {
	u := base + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}
