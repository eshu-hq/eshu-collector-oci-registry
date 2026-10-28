# eshu-collector-oci-registry

Standalone OCI registry collector for Eshu, extracted from the Eshu core
repository (`eshu-hq/eshu#8861`). It serves one claim at a time through the
component-extension host with the process runner: the host passes one
collector-sdk/v1alpha1 request on stdin and the collector returns one result
on stdout. It is pre-release and holds no producer authority until its
reviewed cutover.

## What it collects

For the container-registry repository named by the claim it reads, through
bounded OCI Distribution calls:

- the repository record and every tag in the bounded tag window;
- one manifest or index per tag, with config, layer, and platform detail;
- descriptors referenced by manifests, and descriptors reported as referrers.

It emits the seven core `oci_registry.*` kinds at schema `1.0.0`:
`repository`, `image_tag_observation`, `image_manifest`, `image_index`,
`image_descriptor`, `image_referrer`, and `warning`. Unknown annotations,
non-allowlisted labels, and credential-bearing URLs are replaced or dropped
before emission.

Result states: `complete`; `partial` when the registry hid the Referrers API
or the tag list exceeded the bounded window (both become warning facts);
`retryable` for rate limits, transport errors, and 5xx; `terminal` for auth
denials, missing repositories, invalid configuration, and exhausted-budget
failures. A failed claim emits no facts, and a cancelled claim returns no
result at all. The collector never returns `unchanged`: the host sends no
previous digest, so duplicate detection rides on `generation.freshness_hint`.

## Providers

Seven registry flavors share the Distribution client: Docker Hub, GHCR, ECR
(AWS credentials minted per claim), JFrog Artifactory, Harbor, Google Artifact
Registry, and Azure Container Registry. Provider adapters translate registry
coordinates and credentials; the runtime owns the scan.

## Configuration

`config.example.yaml` is an activation config. The `config` map holds one
repository target — `provider`, `registry`, `repository`, `tag_limit`,
`visibility`, `auth_mode`, `source_uri`, `*_env` credential references — or a
`{"targets":[...]}` list matched by claim scope. Unknown keys fail closed: the
rejection never names the key (it can be a pasted credential), so compare the
config with `config.example.yaml`. `username_env`, `password_env`, and
`bearer_token_env` name environment variables the worker exports; they must be
plain names, and secret values never appear in facts, source URIs, logs, or
statuses. `source_uri` is required: the host validator requires a source URI
on every fact, so a blank one fails fast instead of emitting facts the host
must reject.

The component-extension host forwards the `config` map verbatim as the SDK
request config, so a claim may also carry the standard host envelope keys
alongside the targets. The full shape the host may send is one target object,
or `{"targets":[...], "limits":{...}, "host_github_app_token":{...}}`:

- Top-level `limits` holds the same four budget bounds as a target's own
  `limits` (`max_records_per_claim`, `max_payload_bytes`, `max_result_bytes`,
  `claim_timeout_seconds`), in `snake_case` or `camelCase`
  (`maxRecordsPerClaim`, `maxPayloadBytes`, `maxResultBytes`,
  `claimTimeoutSeconds`) as the host sends them. A target's own bounds win
  where set; the top-level envelope fills the gaps, then the handler bounds,
  then the built-in defaults.
- `host_github_app_token` is the `{token, scope_id}` object the host injects
  per claim when a token provider is configured. This collector accepts and
  ignores it — OCI auth stays environment references — and its value never
  reaches an error, a log line, or a status.

Any other top-level key still fails closed without naming the key.

`allow_empty_successor` (default false) is an explicit operator attestation
that an empty or sharply shrunken successor generation is legitimate for this
repository. Only an attested target stamps its generation `TrustedDeletion`.

A repository with a large tag window returns more than the host's default
4 MiB stdout cap. Raise `process.stdout_limit_bytes` and
`config.limits.max_result_bytes` together; the collector fails terminal with
`result_budget_exceeded` rather than letting the host drop an oversized
result. The `limits` object holds `max_records_per_claim`,
`max_payload_bytes`, `max_result_bytes`, and `claim_timeout_seconds` (or the
`camelCase` equivalents the host forwards); every
field is optional and an absent or non-positive field falls back to the
top-level envelope value, then the handler bounds, then to the built-in defaults.

## Claim time budget

The host sets a claim's deadline to the **initial lease expiry** and kills the
process there. Heartbeats do not extend it. The collector stops starting
provider calls at the deadline minus `min(2s, 5% of the time left)` and
returns retryable `claim_deadline_exceeded` with no facts, so it ends with a
result of its own instead of being killed mid-request.

## Operator signals

The extension host passes one thing from a claim's result to the work item:
the **failure class** (`registry_rate_limited`, `invalid_config`,
`claim_deadline_exceeded`, ...). The worker does not set a status recorder,
so everything else in the result's status row is dropped:

- **Dropped: source latency and fact count.** The collector fills them in, but
  no dashboard or work item shows them.
- **Dropped: `retry_after_seconds`.** The collector sends it on every
  retryable result, but the worker does not honor it yet.
- **Dropped: the stderr log line.** The collector writes one JSON line per
  claim to stderr: the outcome, scope, generation, failure class, operation,
  HTTP status, and fact count — no URLs, bodies, or secrets. The host's
  process runner keeps stderr only as a byte count and drops the text. The
  line is for a local run (`collector --sdk-stdio < request.json`) and for
  tests.
- **No spans or metrics.** The in-tree collector emitted fetch spans and fetch
  counters. This collector has no equivalent. The manifest declares
  `telemetry.metricsPrefix: eshu_dp_oci_registry_collector_`, but nothing in
  this repository emits a metric under it.

## Image and seeding

The image holds one static binary that installs itself, so an init container
needs no shell:

```bash
collector seed --dest /component [--image-ref ghcr.io/eshu-hq/eshu-collector-oci-registry@sha256:<digest>]
```

writes `/component/collector` (mode 0755) and `/component/manifest.yaml`.
`--image-ref REF` (or the `COLLECTOR_IMAGE_REF` environment variable; the flag
wins) stamps the deployed digest-pinned reference into the manifest's artifact
entry, because an image cannot embed its own digest. The repository manifest
keeps the all-zero placeholder digest permanently (`manifest_test.go` fails on
a real one), because it is embedded in the binary and a committed digest could
only describe an older build. `scripts/pin-digest.sh <repo:tag>` only prints
the `repo@sha256:...` reference of a pushed image; it edits nothing.

## Cutover identity

Replacing the in-tree OCI registry collector with this one must not re-key a
single fact. These activation values have to equal what the in-tree collector
uses for the same repository:

| Activation value | Must equal the in-tree value | If it differs |
| --- | --- | --- |
| `host.scope.id` | the in-tree target's `scope_id` (`oci-registry://<registry>/<repository>`) | every stable key and `scope_id` changes: the repository is ingested as a new scope |
| `host.scope.kind` | `container_registry_repository` | the scope no longer matches the in-tree scope kind |
| `host.sourceSystem` | the in-tree source system, `oci_registry` | `source_ref` changes on every fact |
| claim `generation_id` | the in-tree generation id | retries are not idempotent with the in-tree generation |

Stable fact keys and generation fingerprints use the same algorithm as the
in-tree collector (`internal/stableid` pins it against values produced by the
in-tree function), so identical inputs emit identical keys. Run only one of
the two collectors per repository: with the same scope they would write the
same stable keys.

## Producer grants

All seven kinds are core-owned, so the host refuses them without a live
producer grant. `grant.example.json` lists the seven requests. Each grant is
bound to this component id and version and scoped to the manifest's collector
kind (`oci_registry`), not to a repository: core matches a grant's scope
against the collector kinds the manifest declares.
`scripts/record-grants.sh` records them with `eshu component grant` in a
component home. Core issues grants; this repository only ships the request.

## Releasing

`version` in `manifest.yaml` and the git tag must match (`v0.1.0` for
`0.1.0`); `release_version_test.go` enforces both directions, including the
placeholder-digest gate and the tag-filtered release workflow.

## Build and test

```bash
go build ./...
go test ./...
go test -race ./oci/... .
```

`make ci` runs the lint, test, and race lanes CI runs
(`.github/workflows/ci.yml`; the `ci-complete` job aggregates the others).
See `CONTRIBUTING.md` for the lane contract.

`boundary_test.go` fails the build on any import of Eshu internals or another
collector: this module may only use the published SDK modules, the AWS SDK
for ECR credential minting, yaml for manifest shape tests, and itself.

## Copied code

`internal/stableid`, `internal/httpx`, `internal/regfail`, and
`oci/urlauthority.go` are copies of Eshu core functions that are not
importable from a standalone collector; each file says what it copies and
names the SDK helper that should replace it when published
(`eshu-hq/eshu#8851`).

<!-- skip probe: docs-only -->
