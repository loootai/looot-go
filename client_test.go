package looot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "test-token-not-real"

type captured struct {
	Method  string
	Path    string // escaped path as sent
	Query   url.Values
	Header  http.Header
	Body    map[string]any
	RawBody string
}

type reply struct {
	status  int
	body    string
	headers map[string]string
}

// fake is an httptest server that records requests and plays replies in order (the last
// reply repeats).
type fake struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []captured
	replies []reply
}

func newFake(t *testing.T, replies ...reply) *fake {
	t.Helper()
	f := &fake{t: t, replies: replies}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c := captured{Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.Query(), Header: r.Header.Clone(), RawBody: string(raw)}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &c.Body)
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, c)
		rep := reply{status: 200, body: `{}`}
		if len(f.replies) > 0 {
			idx := len(f.reqs) - 1
			if idx >= len(f.replies) {
				idx = len(f.replies) - 1
			}
			rep = f.replies[idx]
		}
		f.mu.Unlock()
		for k, v := range rep.headers {
			w.Header().Set(k, v)
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(rep.status)
		_, _ = io.WriteString(w, rep.body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.reqs) }
func (f *fake) last() captured {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) == 0 {
		f.t.Fatal("no request was sent")
	}
	return f.reqs[len(f.reqs)-1]
}

// client returns a client pointed at the fake, with retries that do not sleep.
func (f *fake) client(opts ...Option) *Client {
	base := []Option{WithBaseURL(f.srv.URL), WithToken(testToken)}
	c := New(append(base, opts...)...)
	c.sleep = func(time.Duration, <-chan struct{}) bool { return true }
	return c
}

var ctx = context.Background()

func ok(body string) reply { return reply{status: 200, body: body} }

// ---------- contract conformance (section 12) ----------

func TestCatalogOverviewSendsTopicAndNoAuthorization(t *testing.T) {
	f := newFake(t, ok(`{"revision":"118131","topic":"email","matchCount":74,"truncated":true,"jobs":[{"id":"people.email.find","providerCount":17,"cheapestPerCall":0.003598,"cheapestPerResult":null}],"futureField":1}`))
	out, err := f.client().CatalogOverview(ctx, &CatalogOverviewParams{Topic: "email"})
	if err != nil {
		t.Fatal(err)
	}
	r := f.last()
	if r.Method != "GET" || r.Path != "/v1/catalog/overview" || r.Query["topic"][0] != "email" {
		t.Fatalf("request = %s %s %v", r.Method, r.Path, r.Query)
	}
	if r.Header.Get("Authorization") != "" {
		t.Fatal("free route sent Authorization")
	}
	if len(r.Query) != 1 {
		t.Fatalf("unset params were sent: %v", r.Query)
	}
	if out.Jobs[0].ID != "people.email.find" || *out.Jobs[0].CheapestPerCall != 0.003598 || out.Jobs[0].CheapestPerResult != nil {
		t.Fatalf("decoded = %+v", out.Jobs[0])
	}
	if !strings.Contains(string(out.RawJSON()), "futureField") {
		t.Fatal("unknown field not kept in RawJSON")
	}
}

func TestSearchSendsBearerMapsParamsAndDropsUnset(t *testing.T) {
	f := newFake(t, ok(`{"query":"verify an email","total":7,"nextOffset":5,"endpoints":[{"endpointId":"icypeas-email-verify","capability":"people.email.verify","estimatedPrice":0.0019,"priceBasis":"perCall","access":"runs_now","works":{"rate":0.97,"runs":120,"p50Ms":380,"thin":false}}]}`))
	yes := true
	max := int64(5000)
	out, err := f.client().Search(ctx, &SearchParams{Q: "verify an email", Prefer: PreferCheapest, MaxPriceMicros: &max, Keyless: &yes})
	if err != nil {
		t.Fatal(err)
	}
	r := f.last()
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		t.Fatal("bearer token missing")
	}
	want := map[string]string{"q": "verify an email", "prefer": "cheapest", "maxPriceMicros": "5000", "keyless": "true"}
	if len(r.Query) != len(want) {
		t.Fatalf("query = %v, want only %v", r.Query, want)
	}
	for k, v := range want {
		if r.Query[k][0] != v {
			t.Fatalf("%s = %q, want %q", k, r.Query[k][0], v)
		}
	}
	if *out.NextOffset != 5 || out.Endpoints[0].EndpointID != "icypeas-email-verify" || out.Endpoints[0].Works.Runs != 120 {
		t.Fatalf("decoded = %+v", out)
	}
}

func TestTokenRouteWithoutTokenSendsNothing(t *testing.T) {
	f := newFake(t)
	c := New(WithBaseURL(f.srv.URL), WithToken(""))
	_, err := c.Balance(ctx)
	var le *Error
	if !errors.As(err, &le) {
		t.Fatalf("err = %v", err)
	}
	if le.Status != 401 || le.Code != "missing_token" {
		t.Fatalf("got %d %s", le.Status, le.Code)
	}
	if le.Message != "GET /v1/balance needs a token: set LOOOT_TOKEN or pass looot.WithToken" {
		t.Fatalf("message = %q", le.Message)
	}
	if f.count() != 0 {
		t.Fatal("a request was sent without a token")
	}
}

func TestRunJobPostsJobIDWaitInQueryAndPreferInFallback(t *testing.T) {
	f := newFake(t, reply{status: 201, body: `{"runId":"run_1","status":"completed","outcome":"hit","requestedJob":{"id":"job:people.email.verify","job":"people.email.verify","prefer":"cheapest","pickedEndpointId":"icypeas-email-verify","skipped":[]},"route":{"servedBy":"icypeas-email-verify","outcome":"hit","chargedUsd":0.0019,"capped":false,"attempts":[{"n":1,"endpointId":"icypeas-email-verify","provider":"icypeas","outcome":"hit","status":"completed","chargedUsd":0.0019,"ms":380}],"skipped":[]}}`})
	run, err := f.client().RunJob(ctx, JobRunRequest{
		Job:            "people.email.verify",
		Input:          map[string]any{"email": "jane.doe@example.com"},
		Prefer:         PreferCheapest,
		Fallback:       &FallbackOptions{MaxAttempts: 3},
		Wait:           20,
		IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := f.last()
	if r.Method != "POST" || r.Path != "/v1/runs" || r.Query["wait"][0] != "20" {
		t.Fatalf("request = %s %s %v", r.Method, r.Path, r.Query)
	}
	if r.Body["endpointId"] != "job:people.email.verify" || r.Body["idempotencyKey"] != "k1" {
		t.Fatalf("body = %s", r.RawBody)
	}
	if _, inBody := r.Body["wait"]; inBody {
		t.Fatal("wait sent in the body")
	}
	if _, top := r.Body["prefer"]; top {
		t.Fatal("prefer sent at the top level")
	}
	fb := r.Body["fallback"].(map[string]any)
	if fb["prefer"] != "cheapest" || fb["maxAttempts"] != float64(3) || len(fb) != 2 {
		t.Fatalf("fallback = %v", fb)
	}
	if run.Route.ServedBy != "icypeas-email-verify" || run.Route.Attempts[0].N != 1 || run.RequestedJob.PickedEndpointID != "icypeas-email-verify" {
		t.Fatalf("decoded = %+v", run)
	}
}

func TestRunJobPreferAloneAndNoFallbackByDefault(t *testing.T) {
	f := newFake(t, reply{status: 201, body: `{"runId":"run_1","status":"queued"}`})
	c := f.client()
	if _, err := c.RunJob(ctx, JobRunRequest{Job: "people.email.find", Input: map[string]any{"domain": "example.com"}, Prefer: PreferReliable}); err != nil {
		t.Fatal(err)
	}
	if got := f.last().RawBody; !strings.Contains(got, `"fallback":{"prefer":"reliable"}`) {
		t.Fatalf("body = %s", got)
	}
	if _, err := c.RunJob(ctx, JobRunRequest{Job: "people.email.find", Input: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if _, has := f.last().Body["fallback"]; has {
		t.Fatal("a job run without prefer or fallback sent fallback")
	}
	if _, err := c.RunJob(ctx, JobRunRequest{Job: "people.email.find", Fallback: FallbackOn()}); err != nil {
		t.Fatal(err)
	}
	if got := f.last().RawBody; !strings.Contains(got, `"fallback":true`) || !strings.Contains(got, `"input":{}`) {
		t.Fatalf("body = %s", got)
	}
}

func TestRunKeepsFallbackObjectAndGeneratesKey(t *testing.T) {
	f := newFake(t, reply{status: 201, body: `{"runId":"run_000028","status":"queued","estimatedCost":0.0019,"reservationId":"res_000025"}`})
	caller := &FallbackOptions{MaxAttempts: 2, MaxCostUSD: 0.05}
	run, err := f.client().Run(ctx, RunRequest{EndpointID: "icypeas-email-verify", Input: map[string]any{"email": "jane.doe@example.com"}, Fallback: caller})
	if err != nil {
		t.Fatal(err)
	}
	r := f.last()
	key, _ := r.Body["idempotencyKey"].(string)
	if !regexp.MustCompile(`^sdk-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(key) {
		t.Fatalf("key = %q", key)
	}
	if r.Header.Get("Idempotency-Key") != key {
		t.Fatal("Idempotency-Key header differs from the body")
	}
	if r.Header.Get("Content-Type") != "application/json" {
		t.Fatal("Content-Type missing on a body")
	}
	if !strings.Contains(r.RawBody, `"fallback":{"maxAttempts":2,"maxCostUsd":0.05}`) {
		t.Fatalf("body = %s", r.RawBody)
	}
	if _, has := r.Query["wait"]; has {
		t.Fatal("wait sent when unset")
	}
	if caller.Prefer != "" {
		t.Fatal("caller's fallback was changed")
	}
	if run.Status != StatusQueued || run.ReservationID != "res_000025" || run.IsTerminal() {
		t.Fatalf("decoded = %+v", run)
	}
}

func TestInsufficientBalanceBecomesError(t *testing.T) {
	f := newFake(t, reply{status: 402, body: `{"error":{"code":"insufficient_balance","message":"Add credit","requestId":"req_1"},"topUp":{"checkoutUrl":"https://checkout.example.com/s/1","dashboardUrl":"https://looot.ai/usage?top_up=1"}}`})
	_, err := f.client().Run(ctx, RunRequest{EndpointID: "x", Input: map[string]any{}})
	var le *LoootError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v", err)
	}
	if le.Status != 402 || le.Code != "insufficient_balance" || le.RequestID != "req_1" || le.Message != "Add credit" {
		t.Fatalf("error = %+v", le)
	}
	if le.Retryable {
		t.Fatal("402 marked retryable")
	}
	if le.TopUp == nil || le.TopUp.URL() != "https://checkout.example.com/s/1" {
		t.Fatalf("topUp = %+v", le.TopUp)
	}
	if f.count() != 1 {
		t.Fatalf("402 was retried: %d requests", f.count())
	}
	if le.Body.(map[string]any)["topUp"] == nil {
		t.Fatal("body not kept")
	}
}

func TestFailedRunWith201IsReturnedNotRaised(t *testing.T) {
	f := newFake(t, reply{status: 201, body: `{"runId":"run_2","status":"failed","outcome":"error","actualCost":0,"result":{"code":"no_supply_for_job"},"error":{"code":"validation_error","message":"No provider supplies this job.","whoseError":"customer","retryable":false,"retryHint":"Pick another job."}}`})
	run, err := f.client().RunJob(ctx, JobRunRequest{Job: "people.unknown", Input: map[string]any{}})
	if err != nil {
		t.Fatalf("failed run raised: %v", err)
	}
	if run.Status != StatusFailed || run.Succeeded() || !run.IsTerminal() {
		t.Fatalf("run = %+v", run)
	}
	if run.Error.Code != "validation_error" || run.Error.RetryHint != "Pick another job." || run.Error.WhoseError != "customer" {
		t.Fatalf("run error = %+v", run.Error)
	}
	var asErr error = run.Error
	if !strings.Contains(asErr.Error(), "validation_error") {
		t.Fatal("RunError.Error() does not name the code")
	}
	if *run.ActualCost != 0 || !strings.Contains(string(run.Result), "no_supply_for_job") {
		t.Fatal("result or cost not decoded")
	}
}

func TestPathSegmentsAreEncoded(t *testing.T) {
	f := newFake(t, ok(`{"runId":"a/b","status":"completed"}`))
	c := f.client()
	if _, err := c.GetRun(ctx, "a/b", nil); err != nil {
		t.Fatal(err)
	}
	if p := f.last().Path; p != "/v1/runs/a%2Fb" {
		t.Fatalf("path = %s", p)
	}
	if _, err := c.Inspect(ctx, "job:people.email.find"); err != nil {
		t.Fatal(err)
	}
	if p := f.last().Path; p != "/v1/operations/job%3Apeople.email.find" {
		t.Fatalf("path = %s", p)
	}
	if _, err := c.CancelRun(ctx, "../balance"); err != nil {
		t.Fatal(err)
	}
	if p := f.last().Path; p != "/v1/runs/..%2Fbalance/cancel" {
		t.Fatalf("path = %s", p)
	}
}

// ---------- every method: route, verb, auth ----------

func TestEveryMethodRoute(t *testing.T) {
	f := newFake(t, ok(`{}`))
	c := f.client()
	cases := []struct {
		name   string
		call   func() error
		method string
		path   string
		auth   bool
		body   bool
	}{
		{"CatalogOverview", func() error { _, e := c.CatalogOverview(ctx, nil); return e }, "GET", "/v1/catalog/overview", false, false},
		{"PublicCatalog", func() error { _, e := c.PublicCatalog(ctx, nil); return e }, "GET", "/v1/public-catalog", false, false},
		{"PublicCatalogItem", func() error { _, e := c.PublicCatalogItem(ctx, "icypeas-email-verify"); return e }, "GET", "/v1/public-catalog/icypeas-email-verify", false, false},
		{"ComparePublicCatalog", func() error { _, e := c.ComparePublicCatalog(ctx, []string{"a", "b"}); return e }, "POST", "/v1/public-catalog/compare", false, true},
		{"Search", func() error { _, e := c.Search(ctx, nil); return e }, "GET", "/v1/catalog/search", true, false},
		{"Discover", func() error { _, e := c.Discover(ctx, nil); return e }, "GET", "/v1/discover", true, false},
		{"CatalogEndpoints", func() error { _, e := c.CatalogEndpoints(ctx, nil); return e }, "GET", "/v1/catalog/endpoints", true, false},
		{"Inspect", func() error { _, e := c.Inspect(ctx, "icypeas-email-verify"); return e }, "GET", "/v1/operations/icypeas-email-verify", true, false},
		{"Run", func() error { _, e := c.Run(ctx, RunRequest{EndpointID: "e"}); return e }, "POST", "/v1/runs", true, true},
		{"RunJob", func() error { _, e := c.RunJob(ctx, JobRunRequest{Job: "web.search"}); return e }, "POST", "/v1/runs", true, true},
		{"GetRun", func() error { _, e := c.GetRun(ctx, "run_1", nil); return e }, "GET", "/v1/runs/run_1", true, false},
		{"ListRuns", func() error { _, e := c.ListRuns(ctx, nil); return e }, "GET", "/v1/runs", true, false},
		{"CancelRun", func() error { _, e := c.CancelRun(ctx, "run_1"); return e }, "POST", "/v1/runs/run_1/cancel", true, false},
		{"RunAttempts", func() error { _, e := c.RunAttempts(ctx, "run_1"); return e }, "GET", "/v1/runs/run_1/attempts", true, false},
		{"Balance", func() error { _, e := c.Balance(ctx); return e }, "GET", "/v1/balance", true, false},
		{"Whoami", func() error { _, e := c.Whoami(ctx); return e }, "GET", "/v1/whoami", true, false},
		{"ListTopUps", func() error { _, e := c.ListTopUps(ctx); return e }, "GET", "/v1/top-ups", true, false},
		{"Ledger", func() error { _, e := c.Ledger(ctx, nil); return e }, "GET", "/v1/ledger", true, false},
		{"CapabilityRequest", func() error {
			_, e := c.CapabilityRequest(ctx, CapabilityRequestParams{Title: "t", Description: "d"})
			return e
		}, "POST", "/v1/capability-requests", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err != nil {
				t.Fatal(err)
			}
			r := f.last()
			if r.Method != tc.method || r.Path != tc.path {
				t.Fatalf("got %s %s, want %s %s", r.Method, r.Path, tc.method, tc.path)
			}
			if got := r.Header.Get("Authorization") != ""; got != tc.auth {
				t.Fatalf("Authorization sent = %v, want %v", got, tc.auth)
			}
			if r.Header.Get("Accept") != "application/json" {
				t.Fatal("Accept missing")
			}
			if !strings.HasPrefix(r.Header.Get("User-Agent"), "looot-go/"+Version+" go/") {
				t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
			}
			if got := r.Header.Get("Content-Type") != ""; got != tc.body {
				t.Fatalf("Content-Type sent = %v, want %v", got, tc.body)
			}
			if r.Header.Get("X-Looot-Agent") != "" {
				t.Fatal("X-Looot-Agent sent without WithAgent")
			}
		})
	}
}

func TestQueryParamsOfExtendedAndPagedMethods(t *testing.T) {
	f := newFake(t, ok(`{}`))
	c := f.client()
	_, _ = c.PublicCatalog(ctx, &PublicCatalogParams{Capability: "people_email_verify", Limit: 1, Cursor: "abc", OperationID: "op"})
	assertQuery(t, f.last(), map[string]string{"capability": "people_email_verify", "limit": "1", "cursor": "abc", "operationId": "op"})

	_, _ = c.ListRuns(ctx, &ListRunsParams{Status: StatusCompleted, Limit: 5, IncludeResult: true})
	assertQuery(t, f.last(), map[string]string{"status": "completed", "limit": "5", "includeResult": "true"})

	_, _ = c.GetRun(ctx, "run_1", &GetRunParams{Fields: []string{"items.email", "total"}, MaxItems: 10})
	assertQuery(t, f.last(), map[string]string{"fields": "items.email,total", "max_items": "10"})

	maxPrice := 0.05
	_, _ = c.Discover(ctx, &DiscoverParams{Query: "enrich a company", Mode: "auto", ResultLimit: 3, MaxPrice: &maxPrice})
	assertQuery(t, f.last(), map[string]string{"query": "enrich a company", "mode": "auto", "resultLimit": "3", "maxPrice": "0.05"})

	no := false
	_, _ = c.CatalogEndpoints(ctx, &CatalogEndpointsParams{Capability: "company.enrich", Facets: true, Verified: &no, Limit: 20})
	assertQuery(t, f.last(), map[string]string{"capability": "company.enrich", "facets": "true", "verified": "false", "limit": "20"})

	_, _ = c.Ledger(ctx, &LedgerParams{Limit: 10, Cursor: "e_1"})
	assertQuery(t, f.last(), map[string]string{"limit": "10", "cursor": "e_1"})

	_, _ = c.ComparePublicCatalog(ctx, []string{"a", "b"})
	if got := f.last().RawBody; got != `{"catalogItemIds":["a","b"]}` {
		t.Fatalf("compare body = %s", got)
	}
}

func assertQuery(t *testing.T, r captured, want map[string]string) {
	t.Helper()
	if len(r.Query) != len(want) {
		t.Fatalf("%s query = %v, want %v", r.Path, r.Query, want)
	}
	for k, v := range want {
		if got := r.Query.Get(k); got != v {
			t.Fatalf("%s %s = %q, want %q", r.Path, k, got, v)
		}
	}
}

func TestDecodesBalanceAndTopUpLink(t *testing.T) {
	f := newFake(t, ok(`{"workspaceId":"ws_1","currency":"USD","available":94.628918,"reserved":0,"topUpLink":{"minimumUsd":5,"suggestedUsd":5,"checkoutUrl":null,"dashboardUrl":"https://looot.ai/usage?top_up=1","reason":"balance_sufficient"}}`))
	b, err := f.client().Balance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b.Available != 94.628918 || *b.TopUpLink.MinimumUSD != 5 || b.TopUpLink.URL() != "https://looot.ai/usage?top_up=1" {
		t.Fatalf("balance = %+v", b)
	}
}

func TestCapabilityRequestGeneratesIDs(t *testing.T) {
	f := newFake(t, reply{status: 201, body: `{"requestId":"x","status":"open"}`})
	if _, err := f.client().CapabilityRequest(ctx, CapabilityRequestParams{Title: "Find a phone", Description: "From a LinkedIn URL", DesiredInputs: []string{"linkedinUrl"}}); err != nil {
		t.Fatal(err)
	}
	b := f.last().Body
	if !strings.HasPrefix(b["requestId"].(string), "sdk-") || !strings.HasPrefix(b["idempotencyKey"].(string), "sdk-") {
		t.Fatalf("body = %v", b)
	}
	if b["desiredInputs"].([]any)[0] != "linkedinUrl" {
		t.Fatalf("body = %v", b)
	}
}

// ---------- errors, retries, transport ----------

func TestRetries429WithSameKeyAndRetryAfter(t *testing.T) {
	f := newFake(t,
		reply{status: 429, body: `{"error":{"code":"too_many_inflight_runs","message":"slow down","requestId":"req_9"}}`, headers: map[string]string{"Retry-After": "2"}},
		reply{status: 201, body: `{"runId":"run_1","status":"completed"}`},
	)
	c := f.client()
	var waits []time.Duration
	c.sleep = func(d time.Duration, _ <-chan struct{}) bool { waits = append(waits, d); return true }
	run, err := c.Run(ctx, RunRequest{EndpointID: "e", Input: map[string]any{}, IdempotencyKey: "lead-1"})
	if err != nil {
		t.Fatal(err)
	}
	if run.RunID != "run_1" || f.count() != 2 {
		t.Fatalf("run = %+v, requests = %d", run, f.count())
	}
	if f.reqs[0].Body["idempotencyKey"] != "lead-1" || f.reqs[1].Body["idempotencyKey"] != "lead-1" {
		t.Fatal("retry changed the idempotency key")
	}
	if len(waits) != 1 || waits[0] != 2*time.Second {
		t.Fatalf("waits = %v", waits)
	}
}

func TestRetryGivesUpAfterMaxAndCapsWait(t *testing.T) {
	f := newFake(t, reply{status: 503, body: `{"error":{"code":"storage_busy","message":"busy"},"retryAfterSeconds":120}`})
	c := f.client()
	var waits []time.Duration
	c.sleep = func(d time.Duration, _ <-chan struct{}) bool { waits = append(waits, d); return true }
	_, err := c.Balance(ctx)
	var le *Error
	if !errors.As(err, &le) || le.Code != "storage_busy" || !le.Retryable || le.RetryAfterSeconds != 120 {
		t.Fatalf("err = %#v", err)
	}
	if f.count() != 1+DefaultMaxRetries {
		t.Fatalf("requests = %d", f.count())
	}
	for _, w := range waits {
		if w != maxRetryWait {
			t.Fatalf("wait %v not capped at 30 s", w)
		}
	}
}

func TestNoRetryOnNonRetryable(t *testing.T) {
	for _, rep := range []reply{
		{status: 400, body: `{"error":{"code":"validation_error","message":"bad"}}`},
		{status: 409, body: `{"error":{"code":"idempotency_conflict","message":"key reused"}}`},
		{status: 503, body: `{"error":{"code":"billing_unavailable","message":"no"}}`},
		{status: 500, body: `{"error":{"code":"internal_error","message":"oops"}}`},
	} {
		f := newFake(t, rep)
		_, err := f.client().Balance(ctx)
		var le *Error
		if !errors.As(err, &le) || le.Status != rep.status {
			t.Fatalf("err = %v", err)
		}
		if f.count() != 1 {
			t.Fatalf("%s retried %d times", le.Code, f.count()-1)
		}
	}
}

func TestBodyRetryableWinsOverTable(t *testing.T) {
	f := newFake(t, reply{status: 429, body: `{"error":{"code":"run_rate_limited","message":"m","retryable":false,"retryHint":"Wait a minute."}}`})
	_, err := f.client().Balance(ctx)
	var le *Error
	errors.As(err, &le)
	if le.Retryable || le.RetryHint != "Wait a minute." || f.count() != 1 {
		t.Fatalf("error = %+v, requests = %d", le, f.count())
	}
}

func TestRetriesOff(t *testing.T) {
	f := newFake(t, reply{status: 429, body: `{"error":{"code":"rate_limit_exceeded","message":"m"}}`})
	_, err := f.client(WithMaxRetries(0)).Balance(ctx)
	if err == nil || f.count() != 1 {
		t.Fatalf("err = %v, requests = %d", err, f.count())
	}
}

func TestNonJSONErrorUsesFallbacks(t *testing.T) {
	f := newFake(t, reply{status: 502, body: `Bad Gateway`, headers: map[string]string{"Content-Type": "text/plain", "X-Request-Id": "req_hdr"}})
	_, err := f.client().Balance(ctx)
	var le *Error
	if !errors.As(err, &le) {
		t.Fatal(err)
	}
	if le.Code != "http_502" || le.Message != "GET /v1/balance failed with HTTP 502" || le.RequestID != "req_hdr" || le.Body != "Bad Gateway" {
		t.Fatalf("error = %+v", le)
	}
}

func TestNetworkErrorIsLoootError(t *testing.T) {
	f := newFake(t)
	f.srv.Close()
	c := f.client(WithMaxRetries(1))
	_, err := c.CatalogOverview(ctx, nil)
	var le *Error
	if !errors.As(err, &le) || le.Status != 0 || le.Code != "network_error" || !le.Retryable || le.Err == nil {
		t.Fatalf("err = %#v", err)
	}
}

func TestTimeoutIsLoootError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL), WithToken(testToken), WithTimeout(50*time.Millisecond), WithMaxRetries(0))
	_, err := c.Balance(ctx)
	var le *Error
	if !errors.As(err, &le) || le.Code != "timeout" || !le.Retryable {
		t.Fatalf("err = %#v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timeout does not unwrap to DeadlineExceeded")
	}
}

func TestCallerCancelStopsWithoutRetry(t *testing.T) {
	f := newFake(t, reply{status: 503, body: `{"error":{"code":"storage_busy","message":"busy"}}`})
	c := f.client()
	cctx, cancel := context.WithCancel(ctx)
	c.sleep = func(time.Duration, <-chan struct{}) bool { cancel(); return false }
	_, err := c.Balance(cctx)
	var le *Error
	if !errors.As(err, &le) || le.Code != "canceled" || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %#v", err)
	}
	if f.count() != 1 {
		t.Fatalf("requests = %d", f.count())
	}
}

func TestTokenNeverInErrorOrString(t *testing.T) {
	f := newFake(t, reply{status: 401, body: `{"error":{"code":"unauthorized","message":"Invalid token","requestId":"req_2"}}`})
	c := f.client()
	_, err := c.Balance(ctx)
	if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(c.String(), testToken) {
		t.Fatalf("token leaked: %v / %s", err, c)
	}
	if !strings.Contains(err.Error(), "unauthorized") || !strings.Contains(err.Error(), "req_2") {
		t.Fatalf("error text = %s", err)
	}
}

// ---------- options ----------

func TestTokenFromEnvAndExplicitEmpty(t *testing.T) {
	t.Setenv("LOOOT_TOKEN", "  env-token  ")
	t.Setenv("LOOOT_API_KEY", "other")
	if New().token != "env-token" {
		t.Fatal("LOOOT_TOKEN not read and trimmed")
	}
	if New(WithToken("")).HasToken() {
		t.Fatal("explicit empty token fell back to the env")
	}
	t.Setenv("LOOOT_TOKEN", "")
	if New().token != "other" {
		t.Fatal("LOOOT_API_KEY not read")
	}
	t.Setenv("LOOOT_API_KEY", "")
	if New().HasToken() {
		t.Fatal("token from nowhere")
	}
}

func TestBaseURLAndAgent(t *testing.T) {
	f := newFake(t, ok(`{}`))
	long := strings.Repeat("a", 80)
	c := New(WithBaseURL(f.srv.URL+"//"), WithAgent(long))
	if c.BaseURL() != f.srv.URL {
		t.Fatalf("base = %s", c.BaseURL())
	}
	if _, err := c.CatalogOverview(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.last().Header.Get("X-Looot-Agent"); got != long[:64] {
		t.Fatalf("agent = %q", got)
	}
	if New().BaseURL() != DefaultBaseURL || DefaultBaseURL != "https://api.looot.ai" {
		t.Fatal("default base URL")
	}
}

// ---------- helpers ----------

func TestWaitForRunPollsUntilTerminal(t *testing.T) {
	f := newFake(t, ok(`{"runId":"run_1","status":"queued"}`), ok(`{"runId":"run_1","status":"running"}`), ok(`{"runId":"run_1","status":"pending_provider"}`), ok(`{"runId":"run_1","status":"completed","outcome":"miss"}`))
	c := f.client()
	var waits []time.Duration
	c.sleep = func(d time.Duration, _ <-chan struct{}) bool { waits = append(waits, d); return true }
	run, err := c.WaitForRun(ctx, "run_1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusCompleted || run.Outcome != OutcomeMiss || f.count() != 4 {
		t.Fatalf("run = %+v after %d polls", run, f.count())
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v", waits)
		}
	}
}

func TestAllRunsFollowsCursor(t *testing.T) {
	f := newFake(t, ok(`{"runs":[{"runId":"r1"},{"runId":"r2"}],"nextCursor":"c2"}`), ok(`{"runs":[{"runId":"r3"}],"nextCursor":null}`))
	var ids []string
	for run, err := range f.client().AllRuns(ctx, &ListRunsParams{Limit: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, run.RunID)
	}
	if strings.Join(ids, ",") != "r1,r2,r3" {
		t.Fatalf("ids = %v", ids)
	}
	if f.reqs[1].Query.Get("cursor") != "c2" || f.reqs[0].Query.Get("cursor") != "" {
		t.Fatal("cursor not followed")
	}
}

func TestAllRunsYieldsError(t *testing.T) {
	f := newFake(t, reply{status: 400, body: `{"error":{"code":"runs_cursor_invalid","message":"m"}}`})
	for _, err := range f.client().AllRuns(ctx, nil) {
		var le *Error
		if !errors.As(err, &le) || le.Code != "runs_cursor_invalid" {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestDownloadMedia(t *testing.T) {
	f := newFake(t, reply{status: 200, body: "PNGDATA", headers: map[string]string{"Content-Type": "image/png"}})
	c := f.client()
	m, err := c.DownloadMedia(ctx, "/v1/runs/run_1/media?part=0")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(m.Body)
	_ = m.Close()
	r := f.last()
	if string(data) != "PNGDATA" || m.ContentType != "image/png" || r.Path != "/v1/runs/run_1/media" || r.Query.Get("part") != "0" || r.Header.Get("Authorization") == "" {
		t.Fatalf("media = %q %s, request = %+v", data, m.ContentType, r)
	}
	for _, bad := range []string{"https://evil.example.com/x", "//evil.example.com/x", "v1/x"} {
		if _, err := c.DownloadMedia(ctx, bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if f.count() != 1 {
		t.Fatal("a rejected path sent a request")
	}
}

func TestRunStatusUnknownValueDoesNotBreak(t *testing.T) {
	f := newFake(t, ok(`{"runId":"r","status":"brand_new_state","outcome":"something"}`))
	run, err := f.client().GetRun(ctx, "r", nil)
	if err != nil || run.Status != "brand_new_state" || run.IsTerminal() {
		t.Fatalf("run = %+v, err = %v", run, err)
	}
}

func TestInvalidJSONOn2xx(t *testing.T) {
	f := newFake(t, ok(`not json`))
	_, err := f.client().Balance(ctx)
	var le *Error
	if !errors.As(err, &le) || le.Code != "invalid_response" {
		t.Fatalf("err = %v", err)
	}
}
