package looot

import (
	"encoding/json"
	"time"
)

// rawSetter is implemented by every top-level response so the full JSON stays reachable.
type rawSetter interface{ setRaw([]byte) }

// rawBody keeps the response exactly as the gateway sent it. The gateway adds fields over
// time; RawJSON lets a caller read one this SDK does not type yet.
type rawBody struct{ raw json.RawMessage }

func (r *rawBody) setRaw(b []byte) { r.raw = append(json.RawMessage(nil), b...) }

// RawJSON returns the response body as received, including fields this SDK does not type.
func (r rawBody) RawJSON() json.RawMessage { return r.raw }

// Prefer ranks providers. Unknown values are passed through as is.
type Prefer string

// Ranking choices for search, discover and fallback.
const (
	PreferCheapest Prefer = "cheapest"
	PreferReliable Prefer = "reliable"
	PreferFastest  Prefer = "fastest"
	PreferBalanced Prefer = "balanced"
)

// RunStatus is a run's lifecycle state. New values may appear; compare with the constants.
type RunStatus string

// Run lifecycle states.
const (
	StatusQueued                RunStatus = "queued"
	StatusRunning               RunStatus = "running"
	StatusPendingProvider       RunStatus = "pending_provider"
	StatusCompleted             RunStatus = "completed"
	StatusFailed                RunStatus = "failed"
	StatusBlocked               RunStatus = "blocked"
	StatusStopped               RunStatus = "stopped"
	StatusReconciliationPending RunStatus = "reconciliation_pending"
)

// IsTerminal reports whether polling can stop. An unknown status counts as not terminal.
func (s RunStatus) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusBlocked, StatusStopped, StatusReconciliationPending:
		return true
	}
	return false
}

// RunOutcome says whether the answer was any good. A completed run can be a miss.
type RunOutcome string

// Run outcomes. route.outcome also uses "failed" and "capped".
const (
	OutcomeHit      RunOutcome = "hit"
	OutcomeWeak     RunOutcome = "weak"
	OutcomeMiss     RunOutcome = "miss"
	OutcomeError    RunOutcome = "error"
	OutcomeRejected RunOutcome = "rejected"
	OutcomeSkipped  RunOutcome = "skipped"
	OutcomePending  RunOutcome = "pending"
	OutcomeFailed   RunOutcome = "failed"
	OutcomeCapped   RunOutcome = "capped"
)

// ---------- catalog overview ----------

// CatalogOverviewParams filters GET /v1/catalog/overview. Every field is optional.
type CatalogOverviewParams struct {
	Depth    string // summary (default), platforms, jobs, full
	Category string
	Platform string
	Topic    string // matching jobs, at most 25
}

// CatalogOverview is the free catalog map: categories by default, jobs with Topic.
type CatalogOverview struct {
	rawBody
	Revision   string           `json:"revision"`
	BuiltAt    string           `json:"builtAt"`
	Depth      string           `json:"depth,omitempty"`
	Totals     *CatalogTotals   `json:"totals,omitempty"`
	Categories []map[string]any `json:"categories,omitempty"`
	Topic      string           `json:"topic,omitempty"`
	MatchCount int              `json:"matchCount,omitempty"`
	Truncated  bool             `json:"truncated,omitempty"`
	Jobs       []OverviewJob    `json:"jobs,omitempty"`
}

// CatalogTotals counts the catalog.
type CatalogTotals struct {
	Categories        int `json:"categories"`
	Platforms         int `json:"platforms"`
	Jobs              int `json:"jobs"`
	Providers         int `json:"providers"`
	Endpoints         int `json:"endpoints"`
	RunnableEndpoints int `json:"runnableEndpoints"`
}

// OverviewJob is one job. Prices are USD; per call and per result are never compared.
type OverviewJob struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Category          string   `json:"category"`
	Platform          string   `json:"platform"`
	ProviderCount     int      `json:"providerCount"`
	EndpointCount     int      `json:"endpointCount"`
	RunnableCount     int      `json:"runnableCount"`
	CheapestPerCall   *float64 `json:"cheapestPerCall"`
	CheapestPerResult *float64 `json:"cheapestPerResult"`
}

// ---------- public catalog ----------

// PublicCatalogParams filters GET /v1/public-catalog. Every field is optional.
type PublicCatalogParams struct {
	Q           string
	Text        string
	Capability  string // job id with dots turned into underscores: people_email_verify
	Category    string
	Provider    string
	OperationID string
	Limit       int // 1 to 100, default 25
	Cursor      string
}

// PublicCatalogPage is one page of the public catalog.
type PublicCatalogPage struct {
	rawBody
	Items          []PublicCatalogItem `json:"items"`
	CatalogDigest  string              `json:"catalogDigest,omitempty"`
	NextCursor     *string             `json:"nextCursor"`
	UnsuppliedJobs []any               `json:"unsuppliedJobs,omitempty"`
	Warnings       []any               `json:"warnings,omitempty"`
}

// PublicCatalogItem is one published catalog item.
type PublicCatalogItem struct {
	rawBody
	CatalogItemID  string             `json:"catalogItemId"`
	Revision       json.RawMessage    `json:"revision,omitempty"`
	ProviderID     string             `json:"providerId"`
	OperationID    string             `json:"operationId"`
	Title          string             `json:"title"`
	Description    string             `json:"description,omitempty"`
	Categories     []string           `json:"categories,omitempty"`
	Capabilities   []string           `json:"capabilities,omitempty"`
	Parameters     []CatalogParameter `json:"parameters,omitempty"`
	Price          *CatalogPrice      `json:"price,omitempty"`
	Verification   json.RawMessage    `json:"verification,omitempty"`
	PublishedAt    string             `json:"publishedAt,omitempty"`
	ItemDigest     string             `json:"itemDigest,omitempty"`
	ProvenanceTier string             `json:"provenanceTier,omitempty"`
}

// CatalogParameter is one input of a public catalog item.
type CatalogParameter struct {
	Name        string `json:"name"`
	Location    string `json:"location,omitempty"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// CatalogPrice is either known (State "known" with AmountMicros) or not (Reason says why).
type CatalogPrice struct {
	State        string `json:"state"`
	AmountMicros *int64 `json:"amountMicros,omitempty"`
	Currency     string `json:"currency,omitempty"`
	Unit         string `json:"unit,omitempty"`
	EffectiveAt  string `json:"effectiveAt,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// CatalogComparison is the answer of POST /v1/public-catalog/compare.
type CatalogComparison struct {
	rawBody
	Items           []PublicCatalogItem `json:"items"`
	PriceComparison json.RawMessage     `json:"priceComparison,omitempty"`
}

// ---------- search, discover, endpoint listing ----------

// SearchParams filters GET /v1/catalog/search. Every field is optional.
type SearchParams struct {
	Q              string
	Limit          int // 1 to 200
	Offset         int
	Category       string
	Platform       string
	Provider       string
	Capability     string // job id with dots: people.email.find
	Keyless        *bool
	Verified       *bool
	Mock           *bool
	MaxPriceMicros *int64
	Prefer         Prefer
}

// SearchResult is one page of search rows.
type SearchResult struct {
	rawBody
	Query      string            `json:"query"`
	Total      int               `json:"total"`
	Endpoints  []CatalogEndpoint `json:"endpoints"`
	NextOffset *int              `json:"nextOffset"`
	Prefer     string            `json:"prefer,omitempty"`
	Warnings   []any             `json:"warnings,omitempty"`
	PriceHint  json.RawMessage   `json:"priceHint,omitempty"`
}

// CatalogEndpoint is one row from search, discover or the endpoint listing.
type CatalogEndpoint struct {
	EndpointID        string          `json:"endpointId"`
	Provider          string          `json:"provider,omitempty"`
	Name              string          `json:"name,omitempty"`
	Capability        string          `json:"capability,omitempty"`
	SourceCapability  string          `json:"sourceCapability,omitempty"`
	Category          string          `json:"category,omitempty"`
	Access            string          `json:"access,omitempty"` // runs_now, needs_your_account, coming_soon
	EstimatedPrice    *float64        `json:"estimatedPrice,omitempty"`
	PriceBasis        string          `json:"priceBasis,omitempty"` // perCall, perResult
	CostPerSuccessUSD *float64        `json:"costPerSuccessUsd,omitempty"`
	Works             *Works          `json:"works,omitempty"`
	Async             bool            `json:"async,omitempty"`
	Credential        json.RawMessage `json:"credential,omitempty"`
	Job               *JobRef         `json:"job,omitempty"`
}

// Works is the measured track record of an endpoint.
type Works struct {
	Rate  *float64 `json:"rate"`
	Runs  int      `json:"runs"`
	P50Ms *int     `json:"p50Ms"`
	Thin  bool     `json:"thin"`
}

// JobRef names a job.
type JobRef struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// DiscoverParams asks GET /v1/discover for ranked candidates.
type DiscoverParams struct {
	Query              string
	Mode               string // auto by default
	Capability         string
	Category           string
	Provider           string
	Prefer             Prefer
	ResultLimit        int // at most 5
	MaxPrice           *float64
	IncludeUnavailable bool
}

// DiscoverResult holds up to 5 candidates.
type DiscoverResult struct {
	rawBody
	Candidates []CatalogEndpoint `json:"candidates"`
	Meta       json.RawMessage   `json:"meta,omitempty"`
}

// CatalogEndpointsParams filters GET /v1/catalog/endpoints, the cursor-paged listing.
type CatalogEndpointsParams struct {
	Capability         string
	Category           string
	Platform           string
	Provider           string
	Keyless            *bool
	Verified           *bool
	Mock               *bool
	MaxPriceMicros     *int64
	IncludeUnavailable bool
	Facets             bool // adds counts
	Limit              int  // 1 to 5000
	Cursor             string
}

// CatalogEndpointsPage is one page of the endpoint listing.
type CatalogEndpointsPage struct {
	rawBody
	Endpoints  []CatalogEndpoint `json:"endpoints"`
	NextCursor *string           `json:"nextCursor"`
	Facets     json.RawMessage   `json:"facets,omitempty"`
}

// ---------- inspect ----------

// Operation is GET /v1/operations/{endpointId}: inputs, price and hints for one endpoint.
type Operation struct {
	rawBody
	Endpoint         OperationEndpoint `json:"endpoint"`
	EstimatedMaxCost json.RawMessage   `json:"estimatedMaxCost,omitempty"`
	PriceRange       json.RawMessage   `json:"priceRange,omitempty"`
	InputSchema      json.RawMessage   `json:"inputSchema,omitempty"`
	RequestBinding   json.RawMessage   `json:"requestBinding,omitempty"`
	UISchema         json.RawMessage   `json:"uiSchema,omitempty"`
	OutputFormat     json.RawMessage   `json:"outputFormat,omitempty"`
	UsageHints       json.RawMessage   `json:"usageHints,omitempty"`
}

// OperationEndpoint is the endpoint part of Operation.
type OperationEndpoint struct {
	ID                   string         `json:"id"`
	Capability           string         `json:"capability,omitempty"`
	EffectClassification string         `json:"effectClassification,omitempty"` // "effectful" marks a write action
	Price                map[string]any `json:"price,omitempty"`
}

// ---------- runs ----------

// FallbackOptions controls fallback across providers. The zero value sends `true`, which
// turns fallback on with the gateway defaults (3 attempts, prefer balanced).
type FallbackOptions struct {
	Enabled         *bool    `json:"enabled,omitempty"`
	MaxAttempts     int      `json:"maxAttempts,omitempty"` // 1 to 10
	MaxCostUSD      float64  `json:"maxCostUsd,omitempty"`  // cap for the whole route, at most 100
	Prefer          Prefer   `json:"prefer,omitempty"`
	Exclude         []string `json:"exclude,omitempty"` // endpoint or provider ids, up to 50
	StopAtFirstMiss bool     `json:"stopAtFirstMiss,omitempty"`
}

// FallbackOn turns fallback on with the gateway defaults. Same as fallback: true.
func FallbackOn() *FallbackOptions { return &FallbackOptions{} }

func (f FallbackOptions) isZero() bool {
	return f.Enabled == nil && f.MaxAttempts == 0 && f.MaxCostUSD == 0 && f.Prefer == "" &&
		len(f.Exclude) == 0 && !f.StopAtFirstMiss
}

// MarshalJSON sends `true` for the zero value and the object otherwise.
func (f FallbackOptions) MarshalJSON() ([]byte, error) {
	if f.isZero() {
		return []byte("true"), nil
	}
	type plain FallbackOptions
	return json.Marshal(plain(f))
}

// RunRequest runs one catalog endpoint. Paid.
type RunRequest struct {
	EndpointID string // a catalog endpoint id, or "job:<job id>"
	Input      any    // a map or struct; nil sends {}
	// IdempotencyKey makes retries safe. Build it from your own record ("lead-4821-email").
	// Empty means the SDK generates "sdk-<uuid>".
	IdempotencyKey string
	Fallback       *FallbackOptions
	Output         any // optional output selection pin
	// Wait is how many seconds (1 to 60) the gateway holds the answer. 0 returns at once.
	Wait int
}

// JobRunRequest runs a job; looot picks the provider. Paid.
type JobRunRequest struct {
	Job            string // a job id such as people.email.find, never invented
	Input          any
	Prefer         Prefer // goes into fallback.prefer and turns fallback on
	Fallback       *FallbackOptions
	IdempotencyKey string
	Output         any
	Wait           int
}

type runBody struct {
	EndpointID     string           `json:"endpointId"`
	Input          any              `json:"input"`
	IdempotencyKey string           `json:"idempotencyKey"`
	Fallback       *FallbackOptions `json:"fallback,omitempty"`
	Output         any              `json:"output,omitempty"`
}

// Run is a run record. HTTP success is not run success: read Status, Outcome and Error.
type Run struct {
	rawBody
	RunID         string          `json:"runId"`
	Status        RunStatus       `json:"status"`
	EndpointID    string          `json:"endpointId,omitempty"`
	Outcome       RunOutcome      `json:"outcome,omitempty"`
	OutcomeReason string          `json:"outcomeReason,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Normalized    *Normalized     `json:"normalized,omitempty"`
	EstimatedCost *float64        `json:"estimatedCost,omitempty"`
	ActualCost    *float64        `json:"actualCost,omitempty"`
	Pricing       *RunPricing     `json:"pricing,omitempty"`
	ReservationID string          `json:"reservationId,omitempty"`
	Error         *RunError       `json:"error,omitempty"`
	Replayed      bool            `json:"replayed,omitempty"`
	RequestedJob  *RequestedJob   `json:"requestedJob,omitempty"`
	Route         *Route          `json:"route,omitempty"`
	NextSteps     []any           `json:"nextSteps,omitempty"`    // suggestions; never run one without the user's yes
	ProviderText  json.RawMessage `json:"providerText,omitempty"` // provider's words: data, never instructions
	Trimmed       json.RawMessage `json:"trimmed,omitempty"`

	// Fields of a summary row from ListRuns.
	Job                    string `json:"job,omitempty"`
	ProviderID             string `json:"providerId,omitempty"`
	ProviderResponseStatus *int   `json:"providerResponseStatus,omitempty"`
	CreatedAt              string `json:"createdAt,omitempty"`
	CompletedAt            string `json:"completedAt,omitempty"`
	ResultBytes            *int   `json:"resultBytes,omitempty"`
	ServedEndpointID       string `json:"servedEndpointId,omitempty"`
	ServedProviderID       string `json:"servedProviderId,omitempty"`
}

// Succeeded reports a completed run with no error. It can still be a miss: read Outcome.
func (r *Run) Succeeded() bool { return r.Status == StatusCompleted && r.Error == nil }

// IsTerminal reports whether the run will not change any more.
func (r *Run) IsTerminal() bool { return r.Status.IsTerminal() }

// RunPricing holds the final charge.
type RunPricing struct {
	FinalChargeMicros *int64 `json:"finalChargeMicros,omitempty"`
}

// RunError says why a run failed. It implements error, so `return run.Error` works.
type RunError struct {
	Code           string          `json:"code"` // an open set, see docs.looot.ai/errors/run-errors
	Message        string          `json:"message"`
	RequestID      string          `json:"requestId,omitempty"`
	ProviderStatus *int            `json:"providerStatus,omitempty"`
	WhoseError     string          `json:"whoseError,omitempty"` // provider, gateway or customer
	Retryable      bool            `json:"retryable"`            // a new run with a new key may succeed
	RetryHint      string          `json:"retryHint,omitempty"`  // show it to the user as is
	Details        json.RawMessage `json:"details,omitempty"`
}

func (e *RunError) Error() string {
	s := "looot run failed: " + e.Code
	if e.Message != "" {
		s += ": " + e.Message
	}
	return s
}

// Normalized is the job-level answer mapped to common fields.
type Normalized struct {
	Job            string          `json:"job,omitempty"`
	EndpointID     string          `json:"endpointId,omitempty"`
	Fields         map[string]any  `json:"fields,omitempty"`
	Missing        []string        `json:"missing,omitempty"`
	Ambiguous      json.RawMessage `json:"ambiguous,omitempty"`
	Unmapped       json.RawMessage `json:"unmapped,omitempty"`
	ProviderValues json.RawMessage `json:"providerValues,omitempty"`
	Verdict        string          `json:"verdict,omitempty"`
	VerdictReason  string          `json:"verdictReason,omitempty"`
	MapVerified    *bool           `json:"mapVerified,omitempty"`
}

// RequestedJob is set on a job run: which endpoint looot picked and why.
type RequestedJob struct {
	ID               string          `json:"id"`
	Job              string          `json:"job"`
	Prefer           string          `json:"prefer,omitempty"`
	PickedEndpointID string          `json:"pickedEndpointId,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	Skipped          json.RawMessage `json:"skipped,omitempty"`
}

// Route is set on a fallback run: every provider tried and what each cost.
type Route struct {
	ServedBy   string         `json:"servedBy,omitempty"`
	Outcome    RunOutcome     `json:"outcome,omitempty"`
	ChargedUSD *float64       `json:"chargedUsd,omitempty"`
	Capped     bool           `json:"capped,omitempty"`
	Attempts   []RouteAttempt `json:"attempts,omitempty"`
	Skipped    []RouteSkip    `json:"skipped,omitempty"`
	Summary    string         `json:"summary,omitempty"`
}

// RouteAttempt is one provider tried by a fallback route.
type RouteAttempt struct {
	N          int        `json:"n"`
	EndpointID string     `json:"endpointId"`
	Provider   string     `json:"provider,omitempty"`
	Outcome    RunOutcome `json:"outcome,omitempty"`
	Status     string     `json:"status,omitempty"`
	ChargedUSD *float64   `json:"chargedUsd,omitempty"`
	Billed     *bool      `json:"billed,omitempty"`
	Ms         *int       `json:"ms,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	Verdict    string     `json:"verdict,omitempty"`
}

// RouteSkip is one candidate a fallback route did not try.
type RouteSkip struct {
	EndpointID string `json:"endpointId"`
	ProviderID string `json:"providerId,omitempty"`
	Code       string `json:"code,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// GetRunParams trims a run's result. The charge does not change.
type GetRunParams struct {
	Fields   []string // dotted paths of result to keep
	MaxItems int      // cap every list, 1 to 10000
}

// ListRunsParams filters GET /v1/runs (newest first).
type ListRunsParams struct {
	Status        RunStatus
	Capability    string // job id with dots
	Limit         int    // 1 to 50, default 20
	Cursor        string
	IncludeResult bool // full run records in place of summaries
}

// RunList is one page of runs.
type RunList struct {
	rawBody
	Runs       []Run   `json:"runs"`
	NextCursor *string `json:"nextCursor"`
}

// RunAttempts lists every durable provider attempt of a run.
type RunAttempts struct {
	rawBody
	Attempts []Attempt `json:"attempts"`
}

// Attempt is one provider call. Other fields stay in RunAttempts.RawJSON.
type Attempt struct {
	ProviderID string `json:"providerId,omitempty"`
	Status     string `json:"status,omitempty"`
	CostMicros *int64 `json:"costMicros,omitempty"`
}

// ---------- money and account ----------

// Balance is the workspace balance. Reading it never opens a payment session.
type Balance struct {
	rawBody
	WorkspaceID       string          `json:"workspaceId"`
	Currency          string          `json:"currency"`
	Available         float64         `json:"available"` // USD
	Reserved          float64         `json:"reserved"`  // USD
	Holds             json.RawMessage `json:"holds,omitempty"`
	TrialCreditMicros *int64          `json:"trialCreditMicros,omitempty"`
	RecentSpendMicros *int64          `json:"recentSpendMicros,omitempty"`
	TopUpLink         *TopUpLink      `json:"topUpLink,omitempty"`
}

// TopUpLink says where the user adds credit. Show CheckoutURL when set, else DashboardURL.
type TopUpLink struct {
	MinimumUSD   *float64 `json:"minimumUsd,omitempty"` // a purchase minimum, not a wallet floor
	SuggestedUSD *float64 `json:"suggestedUsd,omitempty"`
	CheckoutURL  *string  `json:"checkoutUrl,omitempty"`
	DashboardURL string   `json:"dashboardUrl,omitempty"`
	Message      string   `json:"message,omitempty"`
	Reason       string   `json:"reason,omitempty"`
}

// URL returns the checkout URL when one is open, else the dashboard URL.
func (t *TopUpLink) URL() string {
	if t == nil {
		return ""
	}
	if t.CheckoutURL != nil && *t.CheckoutURL != "" {
		return *t.CheckoutURL
	}
	return t.DashboardURL
}

// TopUpList is GET /v1/top-ups.
type TopUpList struct {
	rawBody
	TopUps []map[string]any `json:"topUps"`
}

// LedgerParams pages GET /v1/ledger.
type LedgerParams struct {
	Limit  int // 1 to 50
	Cursor string
}

// LedgerPage is one page of ledger entries, newest first.
type LedgerPage struct {
	rawBody
	Entries    []map[string]any `json:"entries"`
	NextCursor *string          `json:"nextCursor"`
}

// Whoami names the workspace and role behind the token.
type Whoami struct {
	rawBody
	WorkspaceID  string `json:"workspaceId"`
	MembershipID string `json:"membershipId,omitempty"`
	Role         string `json:"role,omitempty"`
}

// CapabilityRequestParams asks looot for a missing provider or job.
type CapabilityRequestParams struct {
	Title          string
	Description    string
	DesiredInputs  []string
	DesiredOutputs []string
	// RequestID and IdempotencyKey are generated when empty.
	RequestID      string
	IdempotencyKey string
}

type capabilityBody struct {
	RequestID      string   `json:"requestId"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	DesiredInputs  []string `json:"desiredInputs,omitempty"`
	DesiredOutputs []string `json:"desiredOutputs,omitempty"`
	IdempotencyKey string   `json:"idempotencyKey"`
}

// CapabilityRequest is the stored request.
type CapabilityRequest struct {
	rawBody
	RequestID string `json:"requestId,omitempty"`
	Status    string `json:"status,omitempty"`
}

// WaitOptions tunes WaitForRun.
type WaitOptions struct {
	// Interval is the first poll delay (default 1 s). It doubles up to MaxInterval (default 5 s).
	Interval    time.Duration
	MaxInterval time.Duration
}
