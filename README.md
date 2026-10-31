# looot for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/loootai/looot-go.svg)](https://pkg.go.dev/github.com/loootai/looot-go) [![License](https://img.shields.io/github/license/loootai/looot-go)](LICENSE) [![Docs](https://img.shields.io/badge/docs-docs.looot.ai-12A06A)](https://docs.looot.ai)

The Go client for the [looot](https://looot.ai) API. looot is one gateway to data providers
(email find and verify, company enrichment, SERP data, social profiles, web scraping) with one
token and one prepaid balance.

Standard library only. Every call takes a `context.Context`. Go 1.23 or newer.

## Install

```bash
go get github.com/loootai/looot-go@v0.1.0
```

Create an agent token at [looot.ai](https://looot.ai) (Settings, Agent tokens) and export it:

```bash
export LOOOT_TOKEN=...
```

## Quickstart

Check the balance, then find a work email. `FallbackOn()` lets looot try the next provider on
a miss. This run is paid.

```go
client := looot.New() // reads LOOOT_TOKEN
bal, err := client.Balance(ctx)
if err != nil { log.Fatal(err) }
if bal.Available < 0.05 { fmt.Println("Add credit:", bal.TopUpLink.URL()); return }
run, err := client.RunJob(ctx, looot.JobRunRequest{
	Job: "people.email.find", Fallback: looot.FallbackOn(), Wait: 30,
	Input: map[string]any{"first_name": "Jane", "last_name": "Doe", "domain": "example.com"},
})
if err != nil { log.Fatal(err) }
fmt.Println(run.Status, run.Outcome, string(run.Result))
```

The full program is in [examples/quickstart](examples/quickstart/main.go). Browsing the
catalog is free and needs no token: see [examples/overview](examples/overview/main.go).

## Read the run, not the HTTP status

A run that failed is not a Go error. `Run` and `RunJob` return it with `Status: "failed"` and
`Error` set, even when the gateway answered HTTP 201. A `completed` run can still be a `miss`.

```go
if run.Error != nil {
	fmt.Println(run.Error.Code, run.Error.RetryHint) // show RetryHint to the user as is
}
```

`run.Error` implements `error`, so `return run.Error` works when you want one.

## Errors

Every non-2xx answer is a `*looot.Error` (alias `looot.LoootError`), and so are a missing
token, a network failure and a timeout (`Status` 0).

```go
var le *looot.Error
if errors.As(err, &le) {
	switch {
	case le.Code == "insufficient_balance":
		fmt.Println("Add credit:", le.TopUp.URL()) // then run again with a new key
	case le.Retryable:
		fmt.Println("Try later, same key:", le.RetryAfterSeconds)
	default:
		fmt.Println(le.Status, le.Code, le.Message, le.RequestID)
	}
}
```

Fields: `Status`, `Code`, `Message`, `RequestID`, `Retryable`, `RetryHint`,
`RetryAfterSeconds`, `TopUp` and the decoded `Body`. The token never appears in an error or in
`client.String()`.

## Idempotency and retries

Every `Run` and `RunJob` sends an idempotency key. Pass your own, built from your record
(`lead-4821-email`), so a crashed worker replays the first run instead of paying twice. With
no key the SDK sends `sdk-<uuid v4>`.

The client retries 429, the retryable 503 codes, network errors and timeouts up to 2 times
with the same key. It waits `Retry-After` when the gateway sends it (capped at 30 s), else a
short backoff. It never retries other 4xx answers. `WithMaxRetries(0)` turns retries off.

After a failed run, or a 402, run again with a new key. The same key only replays the first
answer.

## Options

```go
client := looot.New(
	looot.WithToken(os.Getenv("MY_LOOOT_TOKEN")), // default: LOOOT_TOKEN, then LOOOT_API_KEY
	looot.WithBaseURL("http://localhost:8787"),   // default: https://api.looot.ai
	looot.WithTimeout(90*time.Second),            // per request, default 75 s
	looot.WithMaxRetries(0),                      // default 2
	looot.WithAgent("my-app"),                    // sent as X-Looot-Agent, analytics only
	looot.WithHTTPClient(myHTTPClient),           // for tests or proxies
)
```

`WithToken("")` means no token, even when `LOOOT_TOKEN` is set. Free routes never send the
token.

## Methods

| Method | Route | Token scope | Cost |
| --- | --- | --- | --- |
| `CatalogOverview(ctx, *CatalogOverviewParams)` | `GET /v1/catalog/overview` | none | free |
| `PublicCatalog(ctx, *PublicCatalogParams)` | `GET /v1/public-catalog` | none | free |
| `PublicCatalogItem(ctx, id)` | `GET /v1/public-catalog/{id}` | none | free |
| `ComparePublicCatalog(ctx, ids)` | `POST /v1/public-catalog/compare` | none | free |
| `Search(ctx, *SearchParams)` | `GET /v1/catalog/search` | `catalog.read` | free |
| `Discover(ctx, *DiscoverParams)` | `GET /v1/discover` | `catalog.read` | free |
| `CatalogEndpoints(ctx, *CatalogEndpointsParams)` | `GET /v1/catalog/endpoints` | `catalog.read` | free |
| `Inspect(ctx, endpointID)` | `GET /v1/operations/{id}` | `catalog.read` | free |
| `Run(ctx, RunRequest)` | `POST /v1/runs` | `runs.execute` | paid |
| `RunJob(ctx, JobRunRequest)` | `POST /v1/runs` with `job:<id>` | `runs.execute` | paid |
| `GetRun(ctx, runID, *GetRunParams)` | `GET /v1/runs/{id}` | `runs.read` | free |
| `ListRuns(ctx, *ListRunsParams)` | `GET /v1/runs` | `runs.read` | free |
| `AllRuns(ctx, *ListRunsParams)` | `GET /v1/runs`, every page | `runs.read` | free |
| `WaitForRun(ctx, runID, *WaitOptions)` | `GET /v1/runs/{id}` until terminal | `runs.read` | free |
| `CancelRun(ctx, runID)` | `POST /v1/runs/{id}/cancel` | `runs.execute` | free |
| `RunAttempts(ctx, runID)` | `GET /v1/runs/{id}/attempts` | `runs.read` | free |
| `Balance(ctx)` | `GET /v1/balance` | `usage.read` | free |
| `Whoami(ctx)` | `GET /v1/whoami` | `organizations.read` | free |
| `ListTopUps(ctx)` | `GET /v1/top-ups` | `usage.read` | free |
| `Ledger(ctx, *LedgerParams)` | `GET /v1/ledger` | `usage.read` | free |
| `CapabilityRequest(ctx, CapabilityRequestParams)` | `POST /v1/capability-requests` | any | free |
| `DownloadMedia(ctx, downloadPath)` | `GET <result.media.downloadPath>` | any | free |

Params pointers may be `nil`. Unset fields are not sent. Responses are typed structs, and
`RawJSON()` on each one returns the body as received, so a field the gateway adds later is
still readable.

`RunJob` puts `Prefer` into `fallback.prefer`, which turns fallback on. With neither `Prefer`
nor `Fallback`, a job run does not fall back. Take job ids from `CatalogOverview` or a search
row's `Capability`, never guess one.

To top up, show the user `Balance().TopUpLink.URL()`. An agent token cannot open a payment.

## Develop

```bash
go vet ./...
go test -race ./...
bash scripts/leak-scan.sh .
git config core.hooksPath .githooks   # runs the leak scan before each commit
```

Tests mock HTTP with `httptest`. No test calls the live API.

## Links

- Docs: https://docs.looot.ai
- API reference: https://docs.looot.ai/reference
- Run errors: https://docs.looot.ai/errors/run-errors
- Agent guide: https://looot.ai/skill.md
- Other SDKs: [looot-js](https://github.com/loootai/looot-js), [looot-python](https://github.com/loootai/looot-python)

## License

MIT. See [LICENSE](LICENSE).
