# Publishing looot-go

Go has no registry upload. A Go module is published when a semver tag is on a public GitHub
repo. The Go module proxy (proxy.golang.org) fetches it on the first request and pkg.go.dev
lists it soon after. There is no account, no token and no trusted publisher to set up.

## What is already done

- The repo `loootai/looot-go` is public.
- Tag `v0.1.0` is pushed. The release workflow tests the tag, asks the proxy for it and
  creates the GitHub release.

## Steps for Walid

1. Turn on GitHub Actions for the repo if the org has them off (Settings, Actions, General,
   Allow all actions). Public repos run them for free.
2. Check https://pkg.go.dev/github.com/loootai/looot-go. If the page says "not found", open
   https://proxy.golang.org/github.com/loootai/looot-go/@v/v0.1.0.info once in a browser, wait
   a few minutes, then click "Request" on the pkg.go.dev page.
3. Optional: add the Go SDK to the docs `sdks` page at docs.looot.ai with
   `go get github.com/loootai/looot-go@v0.1.0`.

## Next release

1. Update `Version` in `client.go` and add a section to `CHANGELOG.md`.
2. Run `go vet ./... && go test -race ./...` and `bash scripts/leak-scan.sh .`.
3. Commit, then `git tag v0.1.1 && git push origin main v0.1.1`. The release workflow checks
   that the tag matches `Version`.

Never move or delete a pushed tag. The proxy keeps the first copy forever, so a moved tag
breaks checksums for everyone. Ship a new patch version instead.
