package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRetries is the most retries after the first try. Retries happen
// only on HTTP 429 and 5xx.
const maxRetries = 3

const (
	baseBackoff    = time.Second
	maxBackoff     = 2 * time.Minute
	maxResponseLen = 32 << 20
	maxErrorLen    = 300
)

// Options are the settings shared by every provider client.
type Options struct {
	// BaseURL overrides the provider's default endpoint root.
	BaseURL string
	// Model is the model name sent in the request.
	Model string
	// APIKey is sent in the provider's auth header. It is never put in
	// an error message or a log.
	APIKey string `json:"-"`
	// HTTPClient is the client to use. Nil means a client with a ten
	// minute timeout per attempt.
	HTTPClient *http.Client
	// Sleep waits between retries. Nil means a real, context-aware
	// wait. Tests inject a fake.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock used to read an HTTP-date retry-after. Nil means
	// time.Now.
	Now func() time.Time
}

// poster sends JSON over HTTP with bounded retries. It is shared by
// all three clients.
type poster struct {
	provider string
	client   *http.Client
	sleep    func(ctx context.Context, d time.Duration) error
	now      func() time.Time
	secret   string
}

func newPoster(provider string, o Options) *poster {
	p := &poster{provider: provider, client: o.HTTPClient, sleep: o.Sleep, now: o.Now, secret: o.APIKey}
	if p.client == nil {
		p.client = &http.Client{Timeout: 10 * time.Minute}
	}
	if p.sleep == nil {
		p.sleep = sleepContext
	}
	if p.now == nil {
		p.now = time.Now
	}
	return p
}

// sleepContext waits for d or until ctx ends.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("waiting to retry: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}

// redact removes the API key from s.
func (p *poster) redact(s string) string {
	if len(p.secret) >= 4 {
		s = strings.ReplaceAll(s, p.secret, "[redacted]")
	}
	return s
}

// post sends body to url and returns the body of a 2xx answer. A 429
// or 5xx answer is retried up to maxRetries times, waiting for the
// provider's retry-after when it gives one. Any other failure returns
// at once. errMsg pulls a message out of a provider error body.
func (p *poster) post(ctx context.Context, url string, header map[string]string, body []byte, errMsg func([]byte) string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		status, retryAfter, data, err := p.once(ctx, url, header, body)
		if err != nil {
			return nil, err
		}
		if status >= 200 && status < 300 {
			return data, nil
		}
		apiErr := &APIError{Provider: p.provider, Status: status, Message: p.redact(shorten(errMsg(data), maxErrorLen))}
		retryable := status == http.StatusTooManyRequests || status >= 500
		if !retryable || attempt >= maxRetries {
			return nil, fmt.Errorf("%s request failed after %d attempt(s): %w", p.provider, attempt+1, apiErr)
		}
		wait := backoff(attempt)
		if retryAfter > 0 {
			wait = min(retryAfter, maxBackoff)
		}
		if err := p.sleep(ctx, wait); err != nil {
			return nil, fmt.Errorf("%s: %w", p.provider, err)
		}
	}
}

// once makes one HTTP call.
func (p *poster) once(ctx context.Context, url string, header map[string]string, body []byte) (status int, retryAfter time.Duration, data []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, 0, nil, fmt.Errorf("%s: building request: %w", p.provider, errors.New(p.redact(err.Error())))
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req) // #nosec G704 -- the URL is the base URL the user configured
	if err != nil {
		if ctx.Err() != nil {
			return 0, 0, nil, fmt.Errorf("%s: %w", p.provider, ctx.Err())
		}
		return 0, 0, nil, fmt.Errorf("%s: sending request: %w", p.provider, errors.New(p.redact(err.Error())))
	}
	defer func() { _ = resp.Body.Close() }()
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseLen+1))
	if err != nil {
		if ctx.Err() != nil {
			return 0, 0, nil, fmt.Errorf("%s: %w", p.provider, ctx.Err())
		}
		return 0, 0, nil, fmt.Errorf("%s: reading reply: %w", p.provider, errors.New(p.redact(err.Error())))
	}
	if len(data) > maxResponseLen {
		return 0, 0, nil, fmt.Errorf("%s: reply is larger than %d bytes: %w", p.provider, maxResponseLen, ErrMalformedResponse)
	}
	return resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After"), p.now()), data, nil
}

// backoff is the wait before retry number attempt+1 when the provider
// gives no retry-after: 1s, 2s, 4s.
func backoff(attempt int) time.Duration {
	d := baseBackoff << attempt
	return min(d, maxBackoff)
}

// parseRetryAfter reads a Retry-After header: whole or fractional
// seconds, or an HTTP date. It returns 0 when the header is empty or
// unreadable.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}
