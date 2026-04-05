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

// NewClient returns an HTTPDoer with sensible timeouts.
func NewClient() HTTPDoer {
	return &http.Client{Timeout: 15 * time.Second}
}

// BuildURL constructs a full URL from base, path, and query parameters.
func BuildURL(base, path string, params url.Values) string {
	u := base + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}
