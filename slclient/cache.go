package slclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// CacheRule says how long a successful GET response for one upstream path
// may be served from memory. Path is matched exactly against the request
// URL's path; the cache key is the full URL, so different query strings
// get separate entries.
type CacheRule struct {
	Path string
	TTL  time.Duration
}

// SLCacheRules covers the SL endpoints whose payloads are large and change
// slowly. Catalogs (~1.3 MB sites, ~8 MB stop-points) change with the
// timetable; /v1/messages is a 300+ KB snapshot that every departures and
// trips call re-reads. Real-time endpoints (departures, trips, stop-finder)
// are deliberately absent.
var SLCacheRules = []CacheRule{
	{Path: "/v1/sites", TTL: time.Hour},
	{Path: "/v1/lines", TTL: time.Hour},
	{Path: "/v1/stop-points", TTL: time.Hour},
	{Path: "/v1/transport-authorities", TTL: time.Hour},
	{Path: "/v1/messages", TTL: 30 * time.Second},
}

// NewCachingClient wraps inner with an in-memory response cache for the
// paths in rules. Only 2xx GET responses are stored. Concurrent misses for
// the same URL are coalesced so one upstream fetch serves all of them.
func NewCachingClient(inner HTTPDoer, rules []CacheRule) HTTPDoer {
	return newCachingClient(inner, rules, time.Now)
}

func newCachingClient(inner HTTPDoer, rules []CacheRule, now func() time.Time) *cachingClient {
	ttls := make(map[string]time.Duration, len(rules))
	for _, r := range rules {
		ttls[r.Path] = r.TTL
	}
	return &cachingClient{inner: inner, ttls: ttls, now: now, entries: map[string]*cacheEntry{}}
}

type cachingClient struct {
	inner HTTPDoer
	ttls  map[string]time.Duration
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]*cacheEntry
}

// cacheEntry is created on a miss before the fetch starts; ready is closed
// when the fetch completes so concurrent callers can wait on it.
type cacheEntry struct {
	ready   chan struct{}
	status  int
	header  http.Header
	body    []byte
	err     error
	expires time.Time
}

func (e *cacheEntry) inFlight() bool {
	select {
	case <-e.ready:
		return false
	default:
		return true
	}
}

func (c *cachingClient) Do(req *http.Request) (*http.Response, error) {
	ttl, cacheable := c.ttls[req.URL.Path]
	if !cacheable || req.Method != http.MethodGet {
		return c.inner.Do(req)
	}

	key := req.URL.String()
	entry, owner := c.lookup(key)
	if owner {
		c.fill(entry, req, ttl)
	}

	select {
	case <-entry.ready:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	if entry.err != nil {
		return nil, entry.err
	}
	return &http.Response{
		StatusCode: entry.status,
		Header:     entry.header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(entry.body)),
		Request:    req,
	}, nil
}

// lookup returns the live entry for key, creating one when there is none
// or the existing one has expired. owner is true when the caller created
// the entry and must fill it.
func (c *cachingClient) lookup(key string) (entry *cacheEntry, owner bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[key]; e != nil && (e.inFlight() || c.now().Before(e.expires)) {
		return e, false
	}
	e := &cacheEntry{ready: make(chan struct{})}
	c.entries[key] = e
	return e, true
}

// fill performs the upstream fetch for a fresh entry. The fetch is detached
// from the initiating request's context so one caller's cancellation can't
// fail everyone waiting on the same entry; the inner client's own timeout
// still bounds it. Errors and non-2xx responses are handed to the waiters
// but not kept.
func (c *cachingClient) fill(e *cacheEntry, req *http.Request, ttl time.Duration) {
	defer close(e.ready)
	resp, err := c.inner.Do(req.WithContext(context.WithoutCancel(req.Context())))
	if err != nil {
		e.err = err
		c.forget(req.URL.String(), e)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		e.err = err
		c.forget(req.URL.String(), e)
		return
	}
	e.status, e.header, e.body = resp.StatusCode, resp.Header, body
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.forget(req.URL.String(), e)
		return
	}
	e.expires = c.now().Add(ttl)
}

// forget drops e from the map if it is still the live entry for key.
func (c *cachingClient) forget(key string, e *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries[key] == e {
		delete(c.entries, key)
	}
}
