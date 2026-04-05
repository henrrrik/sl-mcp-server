package slclient

import (
	"net/url"
	"testing"
)

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		path   string
		params url.Values
		want   string
	}{
		{
			name:   "base with path, no params",
			base:   "https://example.com",
			path:   "/v1/messages",
			params: nil,
			want:   "https://example.com/v1/messages",
		},
		{
			name:   "with query params",
			base:   "https://example.com",
			path:   "/v1/messages",
			params: url.Values{"future": {"true"}, "line": {"42"}},
			want:   "https://example.com/v1/messages?future=true&line=42",
		},
		{
			name:   "empty params ignored",
			base:   "https://example.com",
			path:   "/v1/sites",
			params: url.Values{},
			want:   "https://example.com/v1/sites",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildURL(tt.base, tt.path, tt.params)
			if got != tt.want {
				t.Errorf("BuildURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient()
	if c == nil {
		t.Fatal("NewClient() returned nil")
	}
}
