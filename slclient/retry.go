package slclient

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// maxRetryAfterWait caps how long a Retry-After header can make us wait;
// anything longer is treated as "don't bother", and the response is
// returned to the caller with the header intact.
const maxRetryAfterWait = 2 * time.Second

// NewRetryingClient wraps inner so idempotent (GET) requests that fail with
// a transport error, 429 or 5xx are re-attempted, up to attempts in total,
// with backoff between tries (or a small Retry-After when the server sends
// one). Context cancellation and deadlines are never retried. Sits below
// the response cache in main.go so coalesced callers share one retry.
func NewRetryingClient(inner HTTPDoer, attempts int, backoff time.Duration) HTTPDoer {
	if attempts < 1 {
		attempts = 1
	}
	return &retryingClient{inner: inner, attempts: attempts, backoff: backoff}
}

type retryingClient struct {
	inner    HTTPDoer
	attempts int
	backoff  time.Duration
}

func (c *retryingClient) Do(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return c.inner.Do(req)
	}
	for attempt := 1; ; attempt++ {
		resp, err := c.inner.Do(req)
		if !retryable(resp, err) || attempt >= c.attempts {
			return resp, err
		}
		wait := c.backoff
		if resp != nil {
			if ra, ok := retryAfter(resp); ok {
				if ra > maxRetryAfterWait {
					return resp, err
				}
				wait = ra
			}
			resp.Body.Close()
		}
		if err := sleepCtx(req.Context(), wait); err != nil {
			return nil, err
		}
	}
}

// retryable reports whether the outcome is worth another attempt: a
// transport error that isn't the caller giving up, 429, or any 5xx.
func retryable(resp *http.Response, err error) bool {
	if err != nil {
		return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
	}
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

// retryAfter parses a delay-seconds Retry-After header. HTTP-date values
// are ignored (ok=false) — the fallback backoff applies.
func retryAfter(resp *http.Response) (time.Duration, bool) {
	secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || secs < 0 {
		return 0, false
	}
	return time.Duration(secs) * time.Second, true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
