package slclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// scriptedDoer returns the scripted outcomes in order, then repeats the last.
type scriptedDoer struct {
	outcomes []scripted
	calls    int
}

type scripted struct {
	status int
	header http.Header
	err    error
}

func (d *scriptedDoer) Do(req *http.Request) (*http.Response, error) {
	i := d.calls
	if i >= len(d.outcomes) {
		i = len(d.outcomes) - 1
	}
	d.calls++
	o := d.outcomes[i]
	if o.err != nil {
		return nil, o.err
	}
	h := o.header
	if h == nil {
		h = http.Header{}
	}
	return &http.Response{StatusCode: o.status, Header: h, Body: io.NopCloser(strings.NewReader("body"))}, nil
}

func doGet(t *testing.T, c HTTPDoer, ctx context.Context, method string) (*http.Response, error) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, method, "https://x.test/v1/sites", nil)
	return c.Do(req)
}

func TestRetryingClient_RetriesRetryableStatusOnce(t *testing.T) {
	for _, status := range []int{429, 500, 502, 503, 504} {
		inner := &scriptedDoer{outcomes: []scripted{{status: status}, {status: 200}}}
		c := NewRetryingClient(inner, 2, time.Millisecond)
		resp, err := doGet(t, c, context.Background(), http.MethodGet)
		if err != nil || resp.StatusCode != 200 {
			t.Errorf("status %d: expected a successful retry, got %v / %v", status, resp, err)
		}
		if inner.calls != 2 {
			t.Errorf("status %d: expected 2 attempts, got %d", status, inner.calls)
		}
	}
}

func TestRetryingClient_DoesNotRetryClientErrors(t *testing.T) {
	inner := &scriptedDoer{outcomes: []scripted{{status: 400}, {status: 200}}}
	c := NewRetryingClient(inner, 2, time.Millisecond)
	resp, _ := doGet(t, c, context.Background(), http.MethodGet)
	if resp.StatusCode != 400 || inner.calls != 1 {
		t.Errorf("4xx (other than 429) must not be retried: status=%d calls=%d", resp.StatusCode, inner.calls)
	}
}

func TestRetryingClient_GivesUpAfterAttempts(t *testing.T) {
	inner := &scriptedDoer{outcomes: []scripted{{status: 503}}}
	c := NewRetryingClient(inner, 2, time.Millisecond)
	resp, _ := doGet(t, c, context.Background(), http.MethodGet)
	if resp.StatusCode != 503 || inner.calls != 2 {
		t.Errorf("expected the last 503 after 2 attempts, got status=%d calls=%d", resp.StatusCode, inner.calls)
	}
}

func TestRetryingClient_RetriesTransportErrorButNotContextCancel(t *testing.T) {
	inner := &scriptedDoer{outcomes: []scripted{{err: errors.New("connection reset")}, {status: 200}}}
	c := NewRetryingClient(inner, 2, time.Millisecond)
	if resp, err := doGet(t, c, context.Background(), http.MethodGet); err != nil || resp.StatusCode != 200 {
		t.Errorf("transport error should be retried, got %v / %v", resp, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inner = &scriptedDoer{outcomes: []scripted{{err: context.Canceled}, {status: 200}}}
	c = NewRetryingClient(inner, 2, time.Millisecond)
	if _, err := doGet(t, c, ctx, http.MethodGet); err == nil || inner.calls != 1 {
		t.Errorf("a cancelled context must not be retried: err=%v calls=%d", err, inner.calls)
	}
}

func TestRetryingClient_OnlyRetriesGET(t *testing.T) {
	inner := &scriptedDoer{outcomes: []scripted{{status: 503}, {status: 200}}}
	c := NewRetryingClient(inner, 2, time.Millisecond)
	resp, _ := doGet(t, c, context.Background(), http.MethodPost)
	if resp.StatusCode != 503 || inner.calls != 1 {
		t.Errorf("non-idempotent requests must not be retried: status=%d calls=%d", resp.StatusCode, inner.calls)
	}
}

func TestRetryingClient_HonoursSmallRetryAfter(t *testing.T) {
	inner := &scriptedDoer{outcomes: []scripted{{status: 429, header: http.Header{"Retry-After": {"1"}}}, {status: 200}}}
	c := NewRetryingClient(inner, 2, time.Millisecond)
	start := time.Now()
	resp, _ := doGet(t, c, context.Background(), http.MethodGet)
	if resp.StatusCode != 200 {
		t.Fatalf("expected success after Retry-After, got %d", resp.StatusCode)
	}
	if el := time.Since(start); el < 900*time.Millisecond || el > 3*time.Second {
		t.Errorf("expected to wait ~1s for Retry-After: 1, waited %s", el)
	}
}
