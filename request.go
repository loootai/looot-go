package looot

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type apiRequest struct {
	method string
	path   string // already encoded, starts with /v1/
	query  url.Values
	body   any
	auth   bool
	// idempotencyKey, when set, is also sent as the Idempotency-Key header.
	idempotencyKey string
}

// send runs the request with retries and returns the final 2xx response. The caller closes
// the body. Use it only where the body must stream (media download).
func (c *Client) send(ctx context.Context, r apiRequest) (*http.Response, context.CancelFunc, error) {
	if r.auth && c.token == "" {
		return nil, nil, &Error{
			Status:  401,
			Code:    "missing_token",
			Message: fmt.Sprintf("%s %s needs a token: set LOOOT_TOKEN or pass looot.WithToken", r.method, r.path),
			Method:  r.method,
			Path:    r.path,
		}
	}

	var payload []byte
	if r.body != nil {
		var err error
		if payload, err = json.Marshal(r.body); err != nil {
			return nil, nil, fmt.Errorf("looot: encode %s %s body: %w", r.method, r.path, err)
		}
	}

	target := c.baseURL + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}

	for attempt := 0; ; attempt++ {
		resp, cancel, lerr := c.attempt(ctx, r, target, payload)
		if lerr == nil {
			return resp, cancel, nil
		}
		if attempt >= c.maxRetries || !lerr.shouldAutoRetry() || ctx.Err() != nil {
			return nil, nil, lerr
		}
		if !c.sleep(retryDelay(lerr, attempt), ctx.Done()) {
			return nil, nil, canceledError(r, ctx.Err())
		}
	}
}

// attempt sends once. On success it returns the open response and the cancel func of its
// per-attempt timeout; on failure it returns an *Error with the body already read.
func (c *Client) attempt(ctx context.Context, r apiRequest, target string, payload []byte) (*http.Response, context.CancelFunc, *Error) {
	actx, cancel := context.WithTimeout(ctx, c.timeout)

	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(actx, r.method, target, body)
	if err != nil {
		cancel()
		return nil, nil, &Error{Code: "network_error", Message: "build request: " + err.Error(), Method: r.method, Path: r.path, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.auth {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.agent != "" {
		req.Header.Set("X-Looot-Agent", c.agent)
	}
	if r.idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", r.idempotencyKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		cancel()
		return nil, nil, transportError(ctx, r, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, cancel, nil
	}
	data, rerr := io.ReadAll(resp.Body)
	resp.Body.Close()
	cancel()
	if rerr != nil && len(data) == 0 {
		return nil, nil, transportError(ctx, r, rerr)
	}
	return nil, nil, newHTTPError(r.method, r.path, resp, data)
}

// do runs the request and decodes a 2xx JSON body into out (when out is not nil).
func (c *Client) do(ctx context.Context, r apiRequest, out any) error {
	resp, cancel, err := c.send(ctx, r)
	if err != nil {
		return err
	}
	defer cancel()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return transportError(ctx, r, err)
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &Error{
			Status:    resp.StatusCode,
			Code:      "invalid_response",
			Message:   fmt.Sprintf("%s %s returned a body that is not the expected JSON: %v", r.method, r.path, err),
			RequestID: resp.Header.Get("X-Request-Id"),
			Body:      string(data),
			Method:    r.method,
			Path:      r.path,
			Err:       err,
		}
	}
	if rs, ok := out.(rawSetter); ok {
		rs.setRaw(data)
	}
	return nil
}

func transportError(ctx context.Context, r apiRequest, err error) *Error {
	if ctx.Err() != nil {
		return canceledError(r, ctx.Err())
	}
	code, msg := "network_error", fmt.Sprintf("%s %s could not reach looot: %v", r.method, r.path, err)
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		code, msg = "timeout", fmt.Sprintf("%s %s timed out", r.method, r.path)
	}
	return &Error{Code: code, Message: msg, Retryable: true, Method: r.method, Path: r.path, Err: err}
}

func canceledError(r apiRequest, err error) *Error {
	code := "canceled"
	if errors.Is(err, context.DeadlineExceeded) {
		code = "timeout"
	}
	return &Error{Code: code, Message: fmt.Sprintf("%s %s stopped: %v", r.method, r.path, err), Method: r.method, Path: r.path, Err: err}
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

// retryDelay waits Retry-After when the gateway sends it, else 0.5 s, 1 s, 2 s... with jitter.
// Both are capped at 30 s.
func retryDelay(e *Error, attempt int) time.Duration {
	var d time.Duration
	if e.RetryAfterSeconds > 0 {
		d = time.Duration(e.RetryAfterSeconds * float64(time.Second))
	} else {
		d = time.Duration(float64(500*time.Millisecond) * math.Pow(2, float64(attempt)))
		d += time.Duration(randUint32()%250) * time.Millisecond
	}
	if d > maxRetryWait {
		d = maxRetryWait
	}
	return d
}

func randUint32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// newIdempotencyKey returns "sdk-" plus a random UUID v4.
func newIdempotencyKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("sdk-%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// seg encodes one path segment, so "a/b" or "job:x" cannot change the route.
func seg(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), ":", "%3A")
}

// queryBuilder drops empty values, the contract's "never send limit=" rule.
type queryBuilder url.Values

func (q queryBuilder) str(key, v string) {
	if v != "" {
		url.Values(q).Set(key, v)
	}
}

func (q queryBuilder) intPos(key string, v int) {
	if v > 0 {
		url.Values(q).Set(key, strconv.Itoa(v))
	}
}

func (q queryBuilder) boolPtr(key string, v *bool) {
	if v != nil {
		url.Values(q).Set(key, strconv.FormatBool(*v))
	}
}

func (q queryBuilder) boolTrue(key string, v bool) {
	if v {
		url.Values(q).Set(key, "true")
	}
}

func (q queryBuilder) int64Ptr(key string, v *int64) {
	if v != nil {
		url.Values(q).Set(key, strconv.FormatInt(*v, 10))
	}
}

func (q queryBuilder) floatPtr(key string, v *float64) {
	if v != nil {
		url.Values(q).Set(key, strconv.FormatFloat(*v, 'f', -1, 64))
	}
}
