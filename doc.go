// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package collector is the Eshu OCI registry collector, extracted from the
// Eshu core repository (eshu-hq/eshu#8861).
//
// It runs through the Eshu component-extension host with the process runner:
// the host passes one bounded collector-sdk/v1alpha1 request on stdin and the
// collector returns one result on stdout. A claim names one container-registry
// repository scope; the collector reads tags, manifests, indexes, descriptors,
// and referrers through bounded OCI Distribution calls, and emits the seven
// core oci_registry.* fact kinds. It is pre-release and holds no producer
// authority until its reviewed cutover.
//
// Boundary: this package imports only the published SDK modules
// (eshu-sdk/collector, eshu-sdk/factschema), the standard library, AWS SDK
// packages for ECR credential minting, and its own internal helpers. It never
// imports go/internal, another collector implementation, or any core Postgres,
// graph, queue, or lease handle; the import-boundary test enforces this. The
// host, not this package, validates the result and owns claims and commits.
//
// Evidence is redacted before emission: unknown annotations, non-allowlisted
// labels, and credential-bearing URLs are replaced or dropped. Credentials
// are read from environment variables named in the activation config and
// never enter facts, source URIs, logs, or statuses.
package collector
