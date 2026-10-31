package looot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Error is returned for every non-2xx answer, for a missing token, and for network
// failures and timeouts (Status 0). Use errors.As to read it.
//
// A run that failed is not an Error: Run and RunJob return the run with Status "failed".
type Error struct {
	// Status is the HTTP status, or 0 for a network failure or timeout.
	Status int
	// Code is error.code from the body, else "http_<status>". Local codes: "missing_token",
	// "network_error", "timeout", "canceled", "invalid_response".
	Code string
	// Message is error.message, else "<METHOD> <path> failed with HTTP <status>".
	Message string
	// RequestID is error.requestId, else the x-request-id header.
	RequestID string
	// Retryable is true when the same request may succeed later (with the same idempotency key).
	Retryable bool
	// RetryHint is the gateway's one-sentence advice, when it sends one.
	RetryHint string
	// RetryAfterSeconds is the body's retryAfterSeconds, else the Retry-After header. 0 when unset.
	RetryAfterSeconds float64
	// TopUp is set on a 402 insufficient_balance: where the user can add credit.
	TopUp *TopUpLink
	// Body is the decoded JSON body (usually map[string]any), or the raw text if it is not JSON.
	Body any
	// Method and Path name the request that failed.
	Method string
	Path   string
	// Err is the underlying transport error for Status 0.
	Err error
}

// LoootError is the contract name for Error, shared by every looot SDK.
type LoootError = Error

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("looot: ")
	b.WriteString(e.Message)
	b.WriteString(" (")
	b.WriteString(e.Code)
	if e.Status != 0 {
		b.WriteString(", HTTP ")
		b.WriteString(strconv.Itoa(e.Status))
	}
	if e.RequestID != "" {
		b.WriteString(", request ")
		b.WriteString(e.RequestID)
	}
	b.WriteString(")")
	return b.String()
}

// Unwrap returns the transport error, so errors.Is(err, context.DeadlineExceeded) works.
func (e *Error) Unwrap() error { return e.Err }

// retryableByStatus is the contract's table, used when the body does not say.
func retryableByStatus(status int, code string) bool {
	switch status {
	case 429, 500:
		return true
	case 503:
		return code != "billing_unavailable"
	}
	return false
}

// shouldAutoRetry limits automatic retries to 429, retryable 503s and transport failures.
func (e *Error) shouldAutoRetry() bool {
	if !e.Retryable {
		return false
	}
	switch e.Status {
	case 429, 503:
		return true
	case 0:
		return e.Code == "network_error" || e.Code == "timeout"
	}
	return false
}

func newHTTPError(method, path string, resp *http.Response, data []byte) *Error {
	e := &Error{
		Status:    resp.StatusCode,
		Code:      "http_" + strconv.Itoa(resp.StatusCode),
		Message:   fmt.Sprintf("%s %s failed with HTTP %d", method, path, resp.StatusCode),
		RequestID: resp.Header.Get("X-Request-Id"),
		Method:    method,
		Path:      path,
	}
	retryableSet := false

	var body map[string]any
	if len(data) > 0 && json.Unmarshal(data, &body) == nil && body != nil {
		e.Body = body
		if env, ok := body["error"].(map[string]any); ok {
			if s, ok := env["code"].(string); ok && s != "" {
				e.Code = s
			}
			if s, ok := env["message"].(string); ok && s != "" {
				e.Message = s
			}
			if s, ok := env["requestId"].(string); ok && s != "" {
				e.RequestID = s
			}
			if v, ok := env["retryable"].(bool); ok {
				e.Retryable, retryableSet = v, true
			}
			if s, ok := env["retryHint"].(string); ok {
				e.RetryHint = s
			}
			if v, ok := env["retryAfterSeconds"].(float64); ok && v > 0 {
				e.RetryAfterSeconds = v
			}
		}
		if v, ok := body["retryAfterSeconds"].(float64); ok && v > 0 {
			e.RetryAfterSeconds = v
		}
		if s, ok := body["retryHint"].(string); ok && e.RetryHint == "" {
			e.RetryHint = s
		}
		if raw, ok := body["topUp"]; ok && raw != nil {
			if b, err := json.Marshal(raw); err == nil {
				var t TopUpLink
				if json.Unmarshal(b, &t) == nil {
					e.TopUp = &t
				}
			}
		}
	} else if len(data) > 0 {
		e.Body = string(data)
	}

	if e.RetryAfterSeconds == 0 {
		e.RetryAfterSeconds = parseRetryAfter(resp.Header.Get("Retry-After"))
	}
	if !retryableSet {
		e.Retryable = retryableByStatus(e.Status, e.Code)
	}
	return e
}

func parseRetryAfter(v string) float64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
		return n
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t).Seconds(); d > 0 {
			return d
		}
	}
	return 0
}
