# Concepts: how the OCI registry collector works

One run covers one registry repository. Through bounded OCI
Distribution calls (that's the registry read protocol) it reads the
repository record, every tag in a bounded tag window, one manifest or
index per tag, the descriptors those files point to, and referrers. A
manifest describes one image and an index lists variants for different
platforms. Descriptors are layers and config blobs, identified by
digest. Referrers are extra files attached to an image, such as
signatures, reported through the Referrers API where the registry
offers it.

Unknown annotations, labels outside an allowlist, and web addresses
that carry credentials are replaced or dropped before anything leaves
the collector. You won't find them in a fact.

Seven registry flavours share the same read client: Docker Hub, GHCR,
Amazon ECR (where AWS credentials are minted per claim), JFrog
Artifactory, Harbor, Google Artifact Registry, and Azure Container
Registry. Small per-registry adapters translate addresses and logins,
and the scan itself stays shared, so a fix lands once.

## The facts

The full table lives in the [facts reference](../reference/facts.md). It mirrors `manifest.yaml`, so please update the two together; the manifest is the source of truth. All seven kinds are at schema
`1.0.0` with confidence `reported`, meaning the registry said so and
Eshu hasn't checked the bytes itself.


The component is `dev.eshu.collector.oci-registry`, version `0.1.0`,
collector kind `oci_registry`. It declares Eshu core `>=0.0.5 <0.2.0`
and speaks `collector-sdk/v1alpha1` over the process adapter.

## Following one claim through

The step-by-step flow is the
[claim lifecycle diagram](../diagrams/README.md#claim-lifecycle);
the wider picture is the
[system context](../diagrams/README.md#system-context).
A run ends as `complete`, `partial` (the registry hid the Referrers
API or the tag list passed the bounded window; both become warning
facts), `retryable` (rate limits, network errors, server 5xx), or
`terminal` (login refused, missing repository, bad setup, spent
budget). A failed run returns no facts, and a cancelled run returns no
result at all. The collector never returns `unchanged`, because the
host sends no earlier digest; Eshu spots duplicates with a freshness
hint instead.

## Keeping your identity on a switch, and grants

If this replaces Eshu's built-in OCI registry collector for a
repository, a few setup values have to match the old ones or Eshu
treats the same repository as new data. The table is in
[configuration](../reference/configuration.md). Stored keys and fingerprints use
the same method as the built-in collector (tests pin
`internal/stableid` against values from the original), so the same
inputs give you the same keys. Run only one of the two collectors per
repository.

All seven kinds belong to Eshu core, so the host refuses them without
a producer grant, which is written permission for this component to
publish that kind. `grant.example.json` lists the seven requests, each
tied to this component and version and scoped to `oci_registry`, and
`scripts/record-grants.sh` records them (it calls
`eshu component grant`). Core issues grants; this repo only ships the
request.

It never pushes, tags, or deletes anything in a registry, and it never
stores anything itself. It never imports Eshu core internals or
another collector, and `boundary_test.go` fails the build if that
changes. Beyond the SDK it uses the standard library, the AWS SDK for
ECR logins, and YAML parsing in tests. Secrets, credential-bearing
addresses, and unapproved labels stay out of facts, logs, and status
messages. Four small files (`internal/stableid`, `internal/httpx`,
`internal/regfail`, `oci/urlauthority.go`) copy core helpers a
standalone repo can't import yet; each file says what it copies, and
they'll be replaced when shared SDK helpers are published.

## Pictures

The diagrams for this page (system context and claim lifecycle) live
in one place: [diagrams](../diagrams/README.md#system-context) and
[the claim lifecycle](../diagrams/README.md#claim-lifecycle). Each
one has a short text version next to it.
