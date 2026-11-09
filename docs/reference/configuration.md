# Configuration reference: OCI registry collector

Start from the copyable [example config](../../config.example.yaml); component identity lives in the [manifest](../../manifest.yaml). For the facts these settings produce, see [facts](facts.md).

Copy `config.example.yaml`. It has three parts: `host` names the job
identity Eshu plans, `process` says how Eshu starts the binary, and
`config` holds the registry target. The `config` map is either one
target (below) or a `{"targets": [...]}` list, matched to the claim by
scope. Unknown keys are rejected, and the error never names the bad
key because it could be a pasted secret. If a config gets refused,
diff it against the example; that usually finds it faster than
guessing.

## Target settings

| Target key | What it does |
| --- | --- |
| `provider` | Registry flavour, for example `dockerhub` |
| `registry`, `repository` | Registry host and repository path, for example `registry-1.docker.io` and `library/busybox` |
| `tag_limit` | Most tags to read in one claim |
| `visibility`, `auth_mode` | Whether the repository is public or private, and how to log in (for example `anonymous` for a public read) |
| `source_uri` | Required source address recorded on every fact; a blank value fails fast |
| `username_env`, `password_env`, `bearer_token_env` | Names of environment variables that hold the login, see below |
| `allow_empty_successor` | Leave it `false` unless an operator confirms an emptied repository is correct |
| `limits.*` | Size and time caps for one claim, see below |

The host may also send two standard extras next to `targets`: a
top-level `limits` object (the same four caps, in `snake_case` or
`camelCase`; a target's own caps win where set) and a
`host_github_app_token` object, which this collector accepts and
ignores because registry login stays in environment variables. Any
other extra key is still rejected.

`allow_empty_successor: true` is an explicit operator promise that an
empty or much smaller result is correct for that repository and may
replace the old one. Only a target with that promise gets that
treatment, so don't set it casually.

## Logins and credentials

Secrets never go in the config file. `username_env`, `password_env`,
and `bearer_token_env` name environment variables the worker process
already has, and each value must be a plain variable name. For Amazon
ECR, short-lived registry credentials are minted per claim from the
worker's AWS credentials instead, so you don't wire those by hand.
Public repositories can use `auth_mode: anonymous` with no login at
all. Either way, secret values never appear in facts, source
addresses, logs, or status messages.

## Time and size caps

Eshu gives each claim a deadline (the first lease expiry; heartbeat
messages don't extend it) and stops the process there. The collector
stops starting new registry calls shortly before the deadline, the
smaller of 2 seconds or 5% of the time left, and returns `retryable`
with `claim_deadline_exceeded` and no facts. You get a clear result
instead of a process stopped mid-request.

Size caps live under `limits`: `max_records_per_claim`,
`max_payload_bytes` for one fact, `max_result_bytes` for the whole
result, and `claim_timeout_seconds`. The `camelCase` spellings the
host sends also work. Every field is optional; a missing or
non-positive value falls back to the top-level limits, then to
built-in defaults. A repository with many tags can pass the host's
default 4 MiB output cap, so raise `process.stdout_limit_bytes` and
`limits.max_result_bytes` together. The collector fails with
`result_budget_exceeded` rather than let the host drop an oversized
result.

## Switching from the built-in collector

| Setup value | Rule |
| --- | --- |
| `host.scope.id` | Must equal the old scope ID, `oci-registry://<registry>/<repository>`, or every stored key changes |
| `host.scope.kind` | Must be `container_registry_repository` |
| `host.sourceSystem` | Must be `oci_registry`, or the source label changes on every fact |
| Claim generation ID | Reuse the old generation on retry, so a repeated delivery stays a no-op |

## Pins

Exact versions from `go.mod`. Please don't substitute another SDK
line. Unlike the Jira and Confluence collectors, this one really does
build against `eshu-sdk`; that's what `go.mod` requires, and it's why
the SDK repo shows up as a related repo in the README.

| Module | Version |
| --- | --- |
| Go | `1.26.10` |
| `github.com/eshu-hq/eshu-sdk/collector` | `v0.2.0-rc.1` |
| `github.com/eshu-hq/eshu-sdk/factschema` | `v0.1.0-rc.6` |
| `github.com/aws/aws-sdk-go-v2` (+ `config`, `service/ecr`) | `v1.47.4` (ECR login only) |
| `gopkg.in/yaml.v3` | `v3.0.1` |
