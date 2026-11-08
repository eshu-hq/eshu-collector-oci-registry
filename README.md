# eshu-collector-oci-registry

Status: pre-release. Do not rely on it in production yet.

This collector reads one container-image repository from an OCI registry and hands Eshu a checked summary of its tags and image files.

Eshu is the core system. A collector is a small helper program: it reads one outside system, turns what it finds into facts (small, typed records), and gives those facts back to Eshu. OCI is the open standard most container registries speak (Docker Hub, GHCR, ECR, and others). This collector only reads registries. It never pushes, deletes, or changes an image.

## What it collects

One run (a claim, the job Eshu sends) covers one registry repository. Through bounded OCI Distribution calls (the registry read protocol) it reads the repository record, every tag in a bounded tag window, one manifest or index per tag (a manifest describes one image; an index lists variants for different platforms), the descriptors those files point to (layers and config blobs, identified by digest), and referrers (extra files attached to an image, such as signatures, reported through the Referrers API where the registry offers it).

It returns 7 fact kinds, all at schema `1.0.0` with confidence `reported` (the registry said so; Eshu did not check the bytes itself):

| Fact kind | One fact describes |
| --- | --- |
| `oci_registry.repository` | the repository itself |
| `oci_registry.image_tag_observation` | one tag seen in the repository |
| `oci_registry.image_manifest` | one image manifest |
| `oci_registry.image_index` | one multi-platform image index |
| `oci_registry.image_descriptor` | one referenced blob (layer, config, or platform entry) |
| `oci_registry.image_referrer` | one file attached to an image |
| `oci_registry.warning` | something the registry hid or the read had to skip |

Unknown annotations, labels outside an allowlist, and web addresses that carry credentials are replaced or dropped before anything leaves the collector.

A run ends as `complete`, `partial` (the registry hid the Referrers API or the tag list passed the bounded window; both become warning facts), `retryable` (try again later: rate limits, network errors, server 5xx errors), or `terminal` (stop until a person fixes something: login refused, missing repository, bad setup, spent budget). A failed run returns no facts, and a cancelled run returns no result at all. The collector never returns `unchanged`, because the host sends no earlier digest; Eshu spots duplicates with a freshness hint instead.

Seven registry flavors share the same read client: Docker Hub, GHCR, Amazon ECR (AWS credentials are minted per claim), JFrog Artifactory, Harbor, Google Artifact Registry, and Azure Container Registry. Small per-registry adapters translate addresses and logins; the scan itself is shared.

## How data flows

```mermaid
flowchart LR
  A["OCI registry<br/>one repository"] --> B["This collector"] --> C["Eshu host process"] --> D["Eshu core"]
```

Text version: OCI registry repository → this collector → Eshu host process (checks and stores the result) → Eshu core. The collector never talks to Eshu's database directly.

## When to use it, and when not to

Use it when Eshu needs to know which tags, images, and attached files exist in one registry repository, refreshed on a schedule Eshu controls. Do not use it to publish or delete images, to scan image contents layer by layer, or as a general registry browser. It reports registry metadata only, for the repositories you set up.

## How Eshu runs it

Eshu's component host starts this program for one claim at a time: it sends one bounded `collector-sdk/v1alpha1` request on standard input (stdin) and reads one result from standard output (stdout). The collector gets no database, graph, queue, or lease handle; the host checks the result and owns claims and commits.

```bash
collector --sdk-stdio   # serve one claim from stdin
```

The published image is `ghcr.io/eshu-hq/eshu-collector-oci-registry`. It holds one static binary with no shell, and the binary can install itself into a component folder (how a startup, or init, container sets it up):

```bash
collector seed --dest /component [--image-ref ghcr.io/eshu-hq/eshu-collector-oci-registry@sha256:<digest>]
```

This writes `/component/collector` (mode 0755) and `/component/manifest.yaml`. `--image-ref` (or the `COLLECTOR_IMAGE_REF` environment variable; the flag wins) stamps the exact deployed image reference into the manifest, because an image cannot contain its own digest. The manifest in this repository keeps an all-zero placeholder digest on purpose. `scripts/pin-digest.sh <repo:tag>` only prints the `repo@sha256:...` reference of a pushed image; it edits nothing.

## Configuration

Copy `config.example.yaml`. It has three parts: `host` names the job identity Eshu plans, `process` says how Eshu starts the binary, and `config` holds the registry target. The `config` map is either one target (below) or a `{"targets": [...]}` list, matched to the claim by scope. Unknown keys are rejected, and the error never names the bad key (it could be a pasted secret), so compare your file with the example when a config is rejected.

| Target key | Meaning |
| --- | --- |
| `provider` | Registry flavor, for example `dockerhub` |
| `registry`, `repository` | Registry host and repository path, for example `registry-1.docker.io` and `library/busybox` |
| `tag_limit` | Most tags to read in one claim |
| `visibility`, `auth_mode` | Whether the repository is public or private; how to log in (for example `anonymous` for a public read) |
| `source_uri` | Required source address recorded on every fact; a blank value fails fast |
| `username_env`, `password_env`, `bearer_token_env` | Names of environment variables that hold the login (see below) |
| `allow_empty_successor` | Leave `false` unless an operator confirms an emptied repository is correct (see below) |
| `limits.*` | Size and time caps for one claim (see below) |

The host may also send two standard extras next to `targets`: a top-level `limits` object (same four caps, in `snake_case` or `camelCase`; a target's own caps win where set) and a `host_github_app_token` object, which this collector accepts and ignores because registry login stays in environment variables. Any other extra key is still rejected. `allow_empty_successor: true` is an explicit operator promise that an empty or much smaller result is correct for that repository and may replace the old one; only a target with that promise gets that treatment.

## Credentials

Secrets never go in the config file. `username_env`, `password_env`, and `bearer_token_env` name environment variables the worker process already has, and each value must be a plain variable name. For Amazon ECR, short-lived registry credentials are minted per claim from the worker's AWS credentials instead. Public repositories can use `auth_mode: anonymous` with no login at all. Secret values never appear in facts, source addresses, logs, or status messages.

## Time and size limits

Eshu gives each claim a deadline (the first lease expiry; heartbeat messages do not extend it) and stops the process there. The collector stops starting new registry calls shortly before the deadline (the smaller of 2 seconds or 5% of the time left) and returns `retryable` with failure class `claim_deadline_exceeded` and no facts, so it ends with a clear result instead of being stopped mid-request.

Size caps live under `limits`: `max_records_per_claim`, `max_payload_bytes` (one fact), `max_result_bytes` (the whole result), and `claim_timeout_seconds` (the `camelCase` spellings the host sends also work). Every field is optional; a missing or non-positive value falls back to the top-level limits, then to built-in defaults. A repository with many tags can pass the host's default 4 MiB output cap, so raise `process.stdout_limit_bytes` and `limits.max_result_bytes` together. The collector fails with `result_budget_exceeded` rather than let the host drop an oversized result.

## What operators see

Today the host passes one thing from each result to the work item: the failure class, such as `registry_rate_limited`, `invalid_config`, or `claim_deadline_exceeded`. The collector fills in source wait time, fact counts, and `retry_after_seconds` (the wait it asks for before a retry), but the worker does not record or honor them yet, so no dashboard shows them. It also writes one JSON line per claim to standard error (stderr) — outcome, scope, failure class, step, HTTP status, and fact count, with no addresses, bodies, or secrets. The host keeps only the byte count and drops the text, so that line helps only in a local run (`collector --sdk-stdio < request.json`) or in tests. This collector emits no metrics or spans of its own: the manifest declares the prefix `eshu_dp_oci_registry_collector_`, but nothing emits under it yet.

## Keeping the same identity when you switch

If this collector replaces Eshu's built-in OCI registry collector for a repository, these setup values must match the old ones, or Eshu will treat the same repository as new data:

| Setup value | Rule |
| --- | --- |
| `host.scope.id` | Must equal the old scope ID (`oci-registry://<registry>/<repository>`), or every stored key changes |
| `host.scope.kind` | Must be `container_registry_repository` |
| `host.sourceSystem` | Must be `oci_registry`, or the source label changes on every fact |
| Claim generation ID | Reuse the old generation on retry, so a repeated delivery stays a no-op |

Stored keys and fingerprints use the same method as the built-in collector (tests pin `internal/stableid` against values from the original), so the same inputs give the same keys. Run only one of the two collectors per repository.

## Permission to publish facts

All 7 fact kinds belong to Eshu core, so the host refuses them without a producer grant (written permission for this component to publish that kind). `grant.example.json` lists the 7 requests, each tied to this component and version and scoped to `oci_registry`; record them with `scripts/record-grants.sh` (it calls `eshu component grant`). Eshu core issues grants; this repository only ships the request.

## Eshu dependencies

Exact versions from `go.mod`. Do not substitute another SDK line.

| Module | Version |
| --- | --- |
| Go | `1.26.10` |
| `github.com/eshu-hq/eshu-sdk/collector` | `v0.2.0-rc.1` |
| `github.com/eshu-hq/eshu-sdk/factschema` | `v0.1.0-rc.6` |
| `github.com/aws/aws-sdk-go-v2` (+ `config`, `service/ecr`) | `v1.47.4` (ECR login only) |
| `gopkg.in/yaml.v3` | `v3.0.1` |

The component ID is `dev.eshu.collector.oci-registry`, version `0.1.0`. It declares support for Eshu core `>=0.0.5 <0.2.0`, speaks `collector-sdk/v1alpha1` over the process adapter, and uses collector kind `oci_registry` (see `manifest.yaml`).

## What it never does

- It never pushes, tags, or deletes anything in a registry, and it never stores anything itself — the host validates and commits.
- It never imports Eshu core internals or another collector; `boundary_test.go` fails the build if that changes. Beyond the SDK it uses only the standard library, the AWS SDK for ECR logins, and YAML parsing in tests.
- It never puts secrets, credential-bearing addresses, or unapproved labels and annotations into facts, logs, or status messages.

Four small files (`internal/stableid`, `internal/httpx`, `internal/regfail`, `oci/urlauthority.go`) copy Eshu core helpers a standalone repo cannot import yet. Each file says what it copies; they will be replaced when the shared SDK helpers are published.

## Build, test, and release

```bash
go build ./...
go test ./...
go test -race ./oci/... .
make ci   # lint + test + race; the full local check CI runs
```

See `CONTRIBUTING.md` for the lane contract (`.github/workflows/ci.yml`, reported as `go-ci / ci-complete`). For releases, `metadata.version` in `manifest.yaml` and the git tag must match (`v0.1.0` for `0.1.0`); tests enforce this in both directions, including the placeholder-digest rule. The release workflow (`.github/workflows/release.yml`) publishes only when a version tag points at the protected `main` head — merge first, then tag that head. Every push to `main` also publishes the moving `:main`, `:latest`, and `:sha-<long-sha>` image tags, with the same provenance, cosign signing, SBOM, and attestation checks as a version release. Pin real deployments to digests, not moving tags.

## Docs and related projects

- `config.example.yaml` — full setup example; `manifest.yaml` — identity, fact list, and core range; `grant.example.json` — the 7 grant requests.
- [Eshu core](https://github.com/eshu-hq/eshu) — the host that runs this collector.
- [eshu-sdk](https://github.com/eshu-hq/eshu-sdk) — the SDK modules pinned above. [eshu-deploy](https://github.com/eshu-hq/eshu-deploy) — deployment examples and reference setups.

## Contributing

See `CONTRIBUTING.md`. In short: run `make ci` before pushing, keep credentials in environment-variable references only, and never weaken the parity, hostile-input, or boundary tests to land a change.

## License

MIT. See `LICENSE`.
