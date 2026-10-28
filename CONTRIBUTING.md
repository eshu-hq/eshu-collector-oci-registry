# Contributing

The test suite is the product contract: the parity, hostile-input, and
boundary tests must stay green. Never weaken them to land a change.

Credentials travel by env reference, never inline. The handler resolves
credential variables through `Handler.Getenv`; tests inject a stub.
Never log a token, password, URL, or provider body.

## Local checks

You need Go (see `go.mod`), `golangci-lint` v2.12.2, and Docker for the
image lane.

```sh
make ci   # lint + test + race, the whole local contract
```

`make lint` runs `go vet`, whole-repo `golangci-lint run` (config in
`.golangci.yml`, org lint set; no baselines), the gofumpt format check,
`go mod tidy -diff`, and the 500-line cap on `.go` files. `make race`
also enforces the statement-coverage floor (`COVERAGE_FLOOR`, may only
rise toward the 90% finish line). `make actionlint` mirrors the
actionlint lane.

## CI lanes

`.github/workflows/ci.yml` runs a `changes` job first, so a PR only pays
for lanes its paths can affect: Go sources run `lint`, `test`, `race`,
`image` (the Dockerfile builds the binary from them), `boundary`, and
`gosec`; fixtures and embedded config run `test`;
shell scripts run `shell` (plus `test`, which reads
`scripts/record-grants.sh`); `Dockerfile` and manifest changes run
`image`; dependency changes run `govulncheck`; workflow changes run
`actionlint` plus the Go lanes. The lanes execute inside one `go-ci`
call to the shared reusable workflow; `go-ci / ci-complete` is the only
required check: it fails when a needed lane fails and passes when lanes
were legitimately skipped (docs-only PRs).

Weekly, `govulncheck` and `gosec` (advisory until its false-positive
rate is measured) run on a schedule and file an issue on findings; the
findings ride the lane artifacts, never a Security-tab upload, so fork
PRs stay green. Dependabot proposes weekly grouped `gomod` and
`github-actions` bumps.
