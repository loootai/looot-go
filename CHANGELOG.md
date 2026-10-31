# Changelog

## 0.1.0 (2026-10-31)

First release.

- One `Client` for the looot REST API. Standard library only, `context` on every call.
- Core methods: `CatalogOverview`, `PublicCatalog`, `PublicCatalogItem`, `Search`, `Inspect`,
  `Run`, `RunJob`, `GetRun`, `ListRuns`, `CancelRun`, `RunAttempts`, `Balance`.
- Extended methods: `Discover`, `CatalogEndpoints`, `ComparePublicCatalog`, `Whoami`,
  `ListTopUps`, `Ledger`, `CapabilityRequest`, `DownloadMedia`.
- Helpers: `WaitForRun` (polls 1 s to 5 s) and `AllRuns` (a range-over-func iterator).
- `*Error` (alias `LoootError`) for every non-2xx answer, missing token, network error and
  timeout, with `Code`, `Retryable`, `RetryHint`, `RetryAfterSeconds` and the 402 `TopUp` link.
- Retries 429, retryable 503s, network errors and timeouts twice by default with the same
  idempotency key. `WithMaxRetries(0)` turns them off.
- Sends `User-Agent: looot-go/0.1.0 go/<version>` and, with `WithAgent`, `X-Looot-Agent`.
