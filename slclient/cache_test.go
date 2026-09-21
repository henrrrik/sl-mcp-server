package slclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingDoer serves a fixed body and counts calls. gate, when set, blocks
// every call until it is closed — used to prove concurrent misses coalesce.
type countingDoer struct {
	calls  atomic.Int32
	status int
	body   string
	gate   chan struct{}
}

func (d *countingDoer) Do(req *http.Request) (*http.Response, error) {
	d.calls.Add(1)
	if d.gate != nil {
		<-d.gate
	}
	status := d.status
	if status == 0 {
		status = 200
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(d.body)),
	}, nil
}

func get(t *testing.T, c HTTPDoer, rawURL string) (int, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func newTestCache(inner HTTPDoer, now *time.Time) HTTPDoer {
	rules := []CacheRule{{Path: "/v1/sites", TTL: time.Hour}, {Path: "/v1/messages", TTL: 30 * time.Second}}
	return newCachingClient(inner, rules, func() time.Time { return *now })
}

func TestCachingClient_ServesRepeatWithinTTLFromCache(t *testing.T) {
	inner := &countingDoer{body: `[{"id":1}]`}
	now := time.Now()
	c := newTestCache(inner, &now)

	s1, b1 := get(t, c, "https://x.test/v1/sites")
	s2, b2 := get(t, c, "https://x.test/v1/sites")

	if inner.calls.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", inner.calls.Load())
	}
	if s1 != 200 || s2 != 200 || b1 != b2 || b1 != `[{"id":1}]` {
		t.Errorf("cached response differs: %d %q / %d %q", s1, b1, s2, b2)
	}
}

func TestCachingClient_RefetchesAfterTTL(t *testing.T) {
	inner := &countingDoer{body: `[]`}
	now := time.Now()
	c := newTestCache(inner, &now)

	get(t, c, "https://x.test/v1/messages?future=true")
	now = now.Add(31 * time.Second)
	get(t, c, "https://x.test/v1/messages?future=true")

	if inner.calls.Load() != 2 {
		t.Errorf("expected refetch after TTL, got %d upstream calls", inner.calls.Load())
	}
}

func TestCachingClient_KeysOnFullURL(t *testing.T) {
	inner := &countingDoer{body: `[]`}
	now := time.Now()
	c := newTestCache(inner, &now)

	get(t, c, "https://x.test/v1/messages?future=true")
	get(t, c, "https://x.test/v1/messages?future=false")

	if inner.calls.Load() != 2 {
		t.Errorf("different query strings must not share an entry, got %d calls", inner.calls.Load())
	}
}

func TestCachingClient_DoesNotCacheNon2xx(t *testing.T) {
	inner := &countingDoer{body: `boom`, status: 503}
	now := time.Now()
	c := newTestCache(inner, &now)

	s1, _ := get(t, c, "https://x.test/v1/sites")
	s2, _ := get(t, c, "https://x.test/v1/sites")

	if s1 != 503 || s2 != 503 {
		t.Errorf("error status must pass through, got %d / %d", s1, s2)
	}
	if inner.calls.Load() != 2 {
		t.Errorf("non-2xx must not be cached, got %d calls", inner.calls.Load())
	}
}

func TestCachingClient_BypassesUnlistedPaths(t *testing.T) {
	inner := &countingDoer{body: `{}`}
	now := time.Now()
	c := newTestCache(inner, &now)

	get(t, c, "https://x.test/v1/sites/9001/departures")
	get(t, c, "https://x.test/v1/sites/9001/departures")

	if inner.calls.Load() != 2 {
		t.Errorf("real-time endpoints must never be cached, got %d calls", inner.calls.Load())
	}
}

func TestCachingClient_CoalescesConcurrentMisses(t *testing.T) {
	inner := &countingDoer{body: `[]`, gate: make(chan struct{})}
	now := time.Now()
	c := newTestCache(inner, &now)

	const n = 8
	var wg sync.WaitGroup
	started := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started <- struct{}{}
			get(t, c, "https://x.test/v1/sites")
		}()
	}
	for i := 0; i < n; i++ {
		<-started
	}
	// Every goroutine is now either the one upstream call or waiting on it.
	time.Sleep(20 * time.Millisecond)
	close(inner.gate)
	wg.Wait()

	if inner.calls.Load() != 1 {
		t.Errorf("expected concurrent misses to coalesce into 1 upstream call, got %d", inner.calls.Load())
	}
}

func TestNewClient_RaisesIdleConnsPerHost(t *testing.T) {
	c, ok := NewClient().(*http.Client)
	if !ok {
		t.Fatal("NewClient should return *http.Client")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected an *http.Transport so idle connections can be tuned")
	}
	if tr.MaxIdleConnsPerHost < 8 {
		t.Errorf("MaxIdleConnsPerHost=%d; a burst of tool calls to one SL host should reuse connections", tr.MaxIdleConnsPerHost)
	}
}
