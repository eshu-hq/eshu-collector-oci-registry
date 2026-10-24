// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu-collector-oci-registry/internal/stableid"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// ociReferenceDigest is one scanned reference and the manifest digest the
// registry reported for it. Digest digests exist only after buildEnvelopes
// fetches manifests, so the scan records them there and the freshness hint is
// attached to the generation post-scan.
type ociReferenceDigest struct {
	reference string
	digest    string
	// digestWarning is empty when the digest came from the registry response,
	// WarningComputedManifestDigest when it was computed from manifest bytes,
	// or WarningMissingManifestDigest when no digest was available.
	digestWarning string
}

// ociReferrerIdentity is the stable identity of one emitted referrer fact:
// the subject it attests and the referrer descriptor that attests it. It
// covers attestation and signature truth that shares every reference digest,
// so a newly published referrer mints a new generation instead of skipping
// as unchanged (#8003 F1).
type ociReferrerIdentity struct {
	subjectDigest string
	digest        string
	mediaType     string
	artifactType  string
}

// ociWarningOutcome is the stable outcome of one emitted collection warning:
// its code, human message, and subject digest. It covers referrer-API and
// config-blob warning truth that shares every reference digest, so a warning
// appearing, clearing, or changing text mints a new generation (#8003 F1).
type ociWarningOutcome struct {
	code    string
	message string
	digest  string
}

// ociFreshnessHint returns the stable content digest for one repository scan
// (#8003): the sorted reference-to-digest pairs, the per-reference digest
// provenance (so a digest-missing warning cannot masquerade as an unchanged
// tick), the sorted referrer identities (so a new attestation cannot hide
// behind unchanged digests), the sorted warning outcomes (so referrer-API and
// config-blob warning drift cannot hide either), and whether the tag list was
// truncated. It never covers observation timestamps, so identical registry
// state at different ticks shares one hint and the store can skip the
// duplicate generation.
func ociFreshnessHint(references []ociReferenceDigest, referrers []ociReferrerIdentity, warnings []ociWarningOutcome, tagListTruncated bool) string {
	ordered := slices.Clone(references)
	slices.SortFunc(ordered, func(a, b ociReferenceDigest) int {
		return strings.Compare(a.reference, b.reference)
	})
	entries := make([]map[string]any, 0, len(ordered))
	for _, ref := range ordered {
		entries = append(entries, map[string]any{
			"digest":         ref.digest,
			"digest_warning": ref.digestWarning,
			"reference":      ref.reference,
		})
	}
	orderedReferrers := slices.Clone(referrers)
	slices.SortFunc(orderedReferrers, func(a, b ociReferrerIdentity) int {
		if c := strings.Compare(a.subjectDigest, b.subjectDigest); c != 0 {
			return c
		}
		if c := strings.Compare(a.digest, b.digest); c != 0 {
			return c
		}
		if c := strings.Compare(a.mediaType, b.mediaType); c != 0 {
			return c
		}
		return strings.Compare(a.artifactType, b.artifactType)
	})
	referrerEntries := make([]map[string]any, 0, len(orderedReferrers))
	for _, referrer := range orderedReferrers {
		referrerEntries = append(referrerEntries, map[string]any{
			"artifact_type":  referrer.artifactType,
			"digest":         referrer.digest,
			"media_type":     referrer.mediaType,
			"subject_digest": referrer.subjectDigest,
		})
	}
	orderedWarnings := slices.Clone(warnings)
	slices.SortFunc(orderedWarnings, func(a, b ociWarningOutcome) int {
		if c := strings.Compare(a.code, b.code); c != 0 {
			return c
		}
		if c := strings.Compare(a.message, b.message); c != 0 {
			return c
		}
		return strings.Compare(a.digest, b.digest)
	})
	warningEntries := make([]map[string]any, 0, len(orderedWarnings))
	for _, warning := range orderedWarnings {
		warningEntries = append(warningEntries, map[string]any{
			"code":    warning.code,
			"digest":  warning.digest,
			"message": warning.message,
		})
	}
	return stableid.StableID("oci-registry.collection.freshness", map[string]any{
		"references":         entries,
		"referrers":          referrerEntries,
		"tag_list_truncated": tagListTruncated,
		"warning_outcomes":   warningEntries,
	})
}

// ociScanFreshnessHint folds the reference digests plus the referrer and
// warning truth the scan actually emitted, so new attestations and
// referrer/config warning drift mint instead of skipping as unchanged
// (#8003 F1). Call it after the truncation warning is appended so that
// outcome is covered too.
func ociScanFreshnessHint(references []ociReferenceDigest, facts []sdk.Fact, tagListTruncated bool) string {
	return ociFreshnessHint(references, ociReferrerIdentities(facts), ociWarningOutcomes(facts), tagListTruncated)
}

// ociReferrerIdentities extracts the stable referrer identities from the
// emitted envelopes, so the freshness hint covers the referrer truth the scan
// actually reported.
func ociReferrerIdentities(facts []sdk.Fact) []ociReferrerIdentity {
	identities := make([]ociReferrerIdentity, 0, len(facts))
	for _, fact := range facts {
		if fact.Kind != oci.FactKindReferrer {
			continue
		}
		identities = append(identities, ociReferrerIdentity{
			subjectDigest: payloadString(fact.Payload, "subject_digest"),
			digest:        payloadString(fact.Payload, "referrer_digest"),
			mediaType:     payloadString(fact.Payload, "referrer_media_type"),
			artifactType:  payloadString(fact.Payload, "artifact_type"),
		})
	}
	return identities
}

// ociWarningOutcomes extracts the stable warning outcomes from the emitted
// envelopes, so the freshness hint covers the referrer-API and config-blob
// warning truth the scan actually reported.
func ociWarningOutcomes(facts []sdk.Fact) []ociWarningOutcome {
	outcomes := make([]ociWarningOutcome, 0, len(facts))
	for _, fact := range facts {
		if fact.Kind != oci.FactKindWarning {
			continue
		}
		outcomes = append(outcomes, ociWarningOutcome{
			code:    payloadString(fact.Payload, "warning_code"),
			message: payloadString(fact.Payload, "message"),
			digest:  payloadString(fact.Payload, "digest"),
		})
	}
	return outcomes
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return value
}
