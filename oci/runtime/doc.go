// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtime scans configured OCI registry repositories and returns
// scan results for the claim handler.
//
// Source scans configured targets in order, skipping past transient transport
// and retryable-status failures to the next target, while ScanClaim resolves
// one workflow-owned claim to its matching target and reuses the claim
// generation ID so retries stay idempotent. Both call a provider-supplied
// RegistryClient for ping, bounded tag list, manifest, and referrer reads.
// The tag-list contract carries explicit completeness so a short paginated
// response cannot become authoritative absence, including when the bounded
// partial window is empty. The runtime parses OCI and Docker-compatible
// manifest bytes, preserves digest identity, and emits warning facts for
// incomplete collection and non-fatal capability gaps such as missing
// referrers, without placing registry hosts, repository names, tags, or
// digests in log fields.
package runtime
