package looot

import (
	"context"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CatalogOverview maps the catalog: categories by default, matching jobs with Topic.
// Free, no token sent.
func (c *Client) CatalogOverview(ctx context.Context, p *CatalogOverviewParams) (*CatalogOverview, error) {
	q := queryBuilder{}
	if p != nil {
		q.str("depth", p.Depth)
		q.str("category", p.Category)
		q.str("platform", p.Platform)
		q.str("topic", p.Topic)
	}
	return fetch[CatalogOverview](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/catalog/overview", query: url.Values(q)})
}

// PublicCatalog lists published catalog items. Free, no token sent.
func (c *Client) PublicCatalog(ctx context.Context, p *PublicCatalogParams) (*PublicCatalogPage, error) {
	q := queryBuilder{}
	if p != nil {
		q.str("q", p.Q)
		q.str("text", p.Text)
		q.str("capability", p.Capability)
		q.str("category", p.Category)
		q.str("provider", p.Provider)
		q.str("operationId", p.OperationID)
		q.intPos("limit", p.Limit)
		q.str("cursor", p.Cursor)
	}
	return fetch[PublicCatalogPage](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/public-catalog", query: url.Values(q)})
}

// PublicCatalogItem returns one published item. Free, no token sent.
func (c *Client) PublicCatalogItem(ctx context.Context, catalogItemID string) (*PublicCatalogItem, error) {
	return fetch[PublicCatalogItem](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/public-catalog/" + seg(catalogItemID)})
}

// ComparePublicCatalog compares 2 to 20 public items. Free, no token sent.
func (c *Client) ComparePublicCatalog(ctx context.Context, catalogItemIDs []string) (*CatalogComparison, error) {
	body := map[string][]string{"catalogItemIds": catalogItemIDs}
	return fetch[CatalogComparison](ctx, c, apiRequest{method: http.MethodPost, path: "/v1/public-catalog/compare", body: body})
}

// Search finds endpoints for a task in plain words. Needs catalog.read. Free.
func (c *Client) Search(ctx context.Context, p *SearchParams) (*SearchResult, error) {
	q := queryBuilder{}
	if p != nil {
		q.str("q", p.Q)
		q.intPos("limit", p.Limit)
		q.intPos("offset", p.Offset)
		q.str("category", p.Category)
		q.str("platform", p.Platform)
		q.str("provider", p.Provider)
		q.str("capability", p.Capability)
		q.boolPtr("keyless", p.Keyless)
		q.boolPtr("verified", p.Verified)
		q.boolPtr("mock", p.Mock)
		q.int64Ptr("maxPriceMicros", p.MaxPriceMicros)
		q.str("prefer", string(p.Prefer))
	}
	return fetch[SearchResult](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/catalog/search", query: url.Values(q), auth: true})
}

// Discover returns up to 5 ranked candidates for a task. Needs catalog.read. Free.
func (c *Client) Discover(ctx context.Context, p *DiscoverParams) (*DiscoverResult, error) {
	q := queryBuilder{}
	if p != nil {
		q.str("query", p.Query)
		q.str("mode", p.Mode)
		q.str("capability", p.Capability)
		q.str("category", p.Category)
		q.str("provider", p.Provider)
		q.str("prefer", string(p.Prefer))
		q.intPos("resultLimit", p.ResultLimit)
		q.floatPtr("maxPrice", p.MaxPrice)
		q.boolTrue("includeUnavailable", p.IncludeUnavailable)
	}
	return fetch[DiscoverResult](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/discover", query: url.Values(q), auth: true})
}

// CatalogEndpoints lists endpoints page by page. Needs catalog.read. Free.
func (c *Client) CatalogEndpoints(ctx context.Context, p *CatalogEndpointsParams) (*CatalogEndpointsPage, error) {
	q := queryBuilder{}
	if p != nil {
		q.str("capability", p.Capability)
		q.str("category", p.Category)
		q.str("platform", p.Platform)
		q.str("provider", p.Provider)
		q.boolPtr("keyless", p.Keyless)
		q.boolPtr("verified", p.Verified)
		q.boolPtr("mock", p.Mock)
		q.int64Ptr("maxPriceMicros", p.MaxPriceMicros)
		q.boolTrue("includeUnavailable", p.IncludeUnavailable)
		q.boolTrue("facets", p.Facets)
		q.intPos("limit", p.Limit)
		q.str("cursor", p.Cursor)
	}
	return fetch[CatalogEndpointsPage](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/catalog/endpoints", query: url.Values(q), auth: true})
}

// Inspect returns one endpoint's inputs, price and hints. It takes an endpoint id, not
// "job:<id>". Free.
func (c *Client) Inspect(ctx context.Context, endpointID string) (*Operation, error) {
	return fetch[Operation](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/operations/" + seg(endpointID), auth: true})
}

// Run runs one endpoint. Paid. Needs runs.execute.
//
// A run refused before dispatch comes back as a run with Status "failed", not as an error.
// An error means the HTTP call failed (402 insufficient_balance, 429, ...).
func (c *Client) Run(ctx context.Context, r RunRequest) (*Run, error) {
	key := r.IdempotencyKey
	if key == "" {
		key = c.newKey()
	}
	input := r.Input
	if input == nil {
		input = map[string]any{}
	}
	q := queryBuilder{}
	q.intPos("wait", r.Wait)
	return fetch[Run](ctx, c, apiRequest{
		method:         http.MethodPost,
		path:           "/v1/runs",
		query:          url.Values(q),
		body:           runBody{EndpointID: r.EndpointID, Input: input, IdempotencyKey: key, Fallback: r.Fallback, Output: r.Output},
		auth:           true,
		idempotencyKey: key,
	})
}

// RunJob runs a job such as "people.email.find"; looot picks the provider. Paid.
// Prefer goes into fallback.prefer and turns fallback on. Without Prefer and Fallback a job
// run does not fall back.
func (c *Client) RunJob(ctx context.Context, r JobRunRequest) (*Run, error) {
	fb := r.Fallback
	if r.Prefer != "" {
		merged := FallbackOptions{}
		if fb != nil {
			merged = *fb
		}
		merged.Prefer = r.Prefer
		fb = &merged
	}
	return c.Run(ctx, RunRequest{
		EndpointID:     "job:" + r.Job,
		Input:          r.Input,
		IdempotencyKey: r.IdempotencyKey,
		Fallback:       fb,
		Output:         r.Output,
		Wait:           r.Wait,
	})
}

// GetRun returns a run. p may be nil. Needs runs.read.
func (c *Client) GetRun(ctx context.Context, runID string, p *GetRunParams) (*Run, error) {
	q := queryBuilder{}
	if p != nil {
		q.str("fields", strings.Join(p.Fields, ","))
		q.intPos("max_items", p.MaxItems)
	}
	return fetch[Run](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/runs/" + seg(runID), query: url.Values(q), auth: true})
}

// ListRuns returns one page of runs, newest first. Needs runs.read.
func (c *Client) ListRuns(ctx context.Context, p *ListRunsParams) (*RunList, error) {
	q := queryBuilder{}
	if p != nil {
		q.str("status", string(p.Status))
		q.str("capability", p.Capability)
		q.intPos("limit", p.Limit)
		q.str("cursor", p.Cursor)
		q.boolTrue("includeResult", p.IncludeResult)
	}
	return fetch[RunList](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/runs", query: url.Values(q), auth: true})
}

// AllRuns follows nextCursor until the last page. Stop ranging to stop paging.
//
//	for run, err := range client.AllRuns(ctx, nil) { ... }
func (c *Client) AllRuns(ctx context.Context, p *ListRunsParams) iter.Seq2[*Run, error] {
	return func(yield func(*Run, error) bool) {
		params := ListRunsParams{}
		if p != nil {
			params = *p
		}
		for {
			page, err := c.ListRuns(ctx, &params)
			if err != nil {
				yield(nil, err)
				return
			}
			for i := range page.Runs {
				if !yield(&page.Runs[i], nil) {
					return
				}
			}
			if page.NextCursor == nil || *page.NextCursor == "" {
				return
			}
			params.Cursor = *page.NextCursor
		}
	}
}

// CancelRun asks the gateway to stop a run and returns it in its current state. A settled
// run comes back unchanged. Needs runs.execute.
func (c *Client) CancelRun(ctx context.Context, runID string) (*Run, error) {
	return fetch[Run](ctx, c, apiRequest{method: http.MethodPost, path: "/v1/runs/" + seg(runID) + "/cancel", auth: true})
}

// RunAttempts lists every provider attempt of a run. Needs runs.read.
func (c *Client) RunAttempts(ctx context.Context, runID string) (*RunAttempts, error) {
	return fetch[RunAttempts](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/runs/" + seg(runID) + "/attempts", auth: true})
}

// WaitForRun polls GetRun until the run reaches a terminal status. Bound it with the
// context: ctx, cancel := context.WithTimeout(ctx, 2*time.Minute). opts may be nil.
func (c *Client) WaitForRun(ctx context.Context, runID string, opts *WaitOptions) (*Run, error) {
	interval, maxInterval := time.Second, 5*time.Second
	if opts != nil {
		if opts.Interval > 0 {
			interval = opts.Interval
		}
		if opts.MaxInterval > 0 {
			maxInterval = opts.MaxInterval
		}
	}
	for {
		run, err := c.GetRun(ctx, runID, nil)
		if err != nil {
			return run, err
		}
		if run.IsTerminal() {
			return run, nil
		}
		if !c.sleep(interval, ctx.Done()) {
			return run, canceledError(apiRequest{method: http.MethodGet, path: "/v1/runs/" + seg(runID)}, ctx.Err())
		}
		if interval *= 2; interval > maxInterval {
			interval = maxInterval
		}
	}
}

// Balance returns the workspace balance and the top-up link. Needs usage.read. Free.
func (c *Client) Balance(ctx context.Context) (*Balance, error) {
	return fetch[Balance](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/balance", auth: true})
}

// Whoami names the workspace and role behind the token. Needs organizations.read.
func (c *Client) Whoami(ctx context.Context) (*Whoami, error) {
	return fetch[Whoami](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/whoami", auth: true})
}

// ListTopUps lists the workspace's top-ups. Needs usage.read. To add credit, show the user
// Balance().TopUpLink.URL().
func (c *Client) ListTopUps(ctx context.Context) (*TopUpList, error) {
	return fetch[TopUpList](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/top-ups", auth: true})
}

// Ledger returns one page of ledger entries. p may be nil. Needs usage.read.
func (c *Client) Ledger(ctx context.Context, p *LedgerParams) (*LedgerPage, error) {
	q := queryBuilder{}
	if p != nil {
		q.intPos("limit", p.Limit)
		q.str("cursor", p.Cursor)
	}
	return fetch[LedgerPage](ctx, c, apiRequest{method: http.MethodGet, path: "/v1/ledger", query: url.Values(q), auth: true})
}

// CapabilityRequest asks looot to add a missing provider or job.
func (c *Client) CapabilityRequest(ctx context.Context, p CapabilityRequestParams) (*CapabilityRequest, error) {
	body := capabilityBody{
		RequestID:      p.RequestID,
		Title:          p.Title,
		Description:    p.Description,
		DesiredInputs:  p.DesiredInputs,
		DesiredOutputs: p.DesiredOutputs,
		IdempotencyKey: p.IdempotencyKey,
	}
	if body.IdempotencyKey == "" {
		body.IdempotencyKey = c.newKey()
	}
	if body.RequestID == "" {
		body.RequestID = c.newKey()
	}
	return fetch[CapabilityRequest](ctx, c, apiRequest{method: http.MethodPost, path: "/v1/capability-requests", body: body, auth: true})
}

// Media is a downloaded file. The caller closes Body.
type Media struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
	cancel        context.CancelFunc
}

// Close closes the body and releases the request.
func (m *Media) Close() error {
	err := m.Body.Close()
	if m.cancel != nil {
		m.cancel()
	}
	return err
}

// DownloadMedia fetches a run's file from result.media.downloadPath. The path must be a
// path on the gateway (starting with "/"), so the token never goes to another host.
func (c *Client) DownloadMedia(ctx context.Context, downloadPath string) (*Media, error) {
	if !strings.HasPrefix(downloadPath, "/") || strings.HasPrefix(downloadPath, "//") {
		return nil, fmt.Errorf("looot: download path must start with a single \"/\", got %q", downloadPath)
	}
	path, rawQuery, _ := strings.Cut(downloadPath, "?")
	var q url.Values
	if rawQuery != "" {
		parsed, err := url.ParseQuery(rawQuery)
		if err != nil {
			return nil, fmt.Errorf("looot: download path query: %w", err)
		}
		q = parsed
	}
	resp, cancel, err := c.send(ctx, apiRequest{method: http.MethodGet, path: path, query: q, auth: true})
	if err != nil {
		return nil, err
	}
	return &Media{Body: resp.Body, ContentType: resp.Header.Get("Content-Type"), ContentLength: resp.ContentLength, cancel: cancel}, nil
}

// fetch sends the request and decodes the answer into a new T. On error it returns nil.
func fetch[T any](ctx context.Context, c *Client, r apiRequest) (*T, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	out := new(T)
	if err := c.do(ctx, r, out); err != nil {
		return nil, err
	}
	return out, nil
}
