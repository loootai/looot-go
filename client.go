// Package looot is the Go client for the looot REST API (https://looot.ai).
//
// looot is one gateway to many data provider operations (email find and verify, company
// enrichment, SERP data, social profiles, web scraping) behind one token and one prepaid
// balance. Reference: https://docs.looot.ai/reference.
//
// Create a client with New, then call its methods with a context:
//
//	client := looot.New() // reads LOOOT_TOKEN
//	bal, err := client.Balance(ctx)
//
// HTTP errors come back as *Error. A run that failed is not an HTTP error: Run and RunJob
// return the run, and the caller reads Status, Outcome and Error on it.
package looot

import (
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// Version is the SDK version sent in the User-Agent header.
const Version = "0.1.0"

// DefaultBaseURL is the production looot API.
const DefaultBaseURL = "https://api.looot.ai"

// DefaultTimeout is the per-request timeout. It covers a run with the longest wait (60 s).
const DefaultTimeout = 75 * time.Second

// DefaultMaxRetries is how many times a retryable error (429, a retryable 503, a network
// error or a timeout) is retried with the same idempotency key.
const DefaultMaxRetries = 2

// maxAgentLength caps the X-Looot-Agent header.
const maxAgentLength = 64

// maxRetryWait caps the wait between two retries.
const maxRetryWait = 30 * time.Second

// Client calls the looot API. It is safe for concurrent use.
type Client struct {
	baseURL    string
	token      string
	agent      string
	userAgent  string
	timeout    time.Duration
	maxRetries int
	httpClient *http.Client
	sleep      func(d time.Duration, done <-chan struct{}) bool
	newKey     func() string
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the bearer token. It wins over the LOOOT_TOKEN environment variable, even
// when it is empty: an explicit empty token means "no token".
func WithToken(token string) Option {
	return func(c *Client) { c.token = strings.TrimSpace(token) }
}

// WithBaseURL points the client at another gateway, for example a local one.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/") }
}

// WithHTTPClient replaces the HTTP client, for tests or custom transports.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithTimeout sets the per-request timeout. Zero or less keeps the default.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithMaxRetries sets how many times a retryable error is retried. 0 turns retries off.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// WithAgent names the app or agent making the calls (for example "n8n"). looot uses it for
// usage analytics only. It is cut to 64 characters and sent as X-Looot-Agent.
func WithAgent(name string) Option {
	return func(c *Client) {
		name = strings.TrimSpace(name)
		if len(name) > maxAgentLength {
			name = name[:maxAgentLength]
		}
		c.agent = name
	}
}

// New returns a client. Without WithToken it reads LOOOT_TOKEN, then LOOOT_API_KEY.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:    DefaultBaseURL,
		token:      envToken(),
		userAgent:  "looot-go/" + Version + " go/" + strings.TrimPrefix(runtime.Version(), "go"),
		timeout:    DefaultTimeout,
		maxRetries: DefaultMaxRetries,
		httpClient: &http.Client{},
		sleep:      sleepOrDone,
		newKey:     newIdempotencyKey,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// BaseURL returns the gateway URL this client calls.
func (c *Client) BaseURL() string { return c.baseURL }

// HasToken reports whether the client has a token. It never returns the token itself.
func (c *Client) HasToken() bool { return c.token != "" }

// String describes the client without the token.
func (c *Client) String() string {
	state := "no token"
	if c.token != "" {
		state = "token set"
	}
	return "looot.Client{" + c.baseURL + ", " + state + "}"
}

func envToken() string {
	for _, name := range []string{"LOOOT_TOKEN", "LOOOT_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

func sleepOrDone(d time.Duration, done <-chan struct{}) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-done:
		return false
	}
}
