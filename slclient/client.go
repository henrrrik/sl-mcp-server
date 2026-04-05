package slclient

import (
	"net/http"
	"net/url"
)

// HTTPDoer abstracts HTTP requests for testability.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// NewClient returns an HTTPDoer backed by http.DefaultClient.
func NewClient() HTTPDoer {
	return http.DefaultClient
}

// BuildURL constructs a full URL from base, path, and query parameters.
func BuildURL(base, path string, params url.Values) string {
	u := base + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}
