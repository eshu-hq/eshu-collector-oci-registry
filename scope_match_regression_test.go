// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestResolveTargetEnforcesScopeInSingleTargetMode proves single-target mode
// enforces claim-scope matching: a claim for another repository is rejected
// instead of scanning the configured target as-is.
func TestResolveTargetEnforcesScopeInSingleTargetMode(t *testing.T) {
	t.Parallel()

	config := validTargetConfig()
	if _, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" }); err != nil {
		t.Fatalf("resolveTarget(matching) error = %v, want the configured target", err)
	}
	_, err := resolveTarget(config, "oci-registry://ghcr.io/eshu-hq/other", func(string) string { return "" })
	if err == nil {
		t.Fatal("resolveTarget(mismatched single target) error = nil, want scope rejection")
	}
	if !strings.Contains(err.Error(), "no configured target matches scope_id") {
		t.Fatalf("rejection = %v, want scope mismatch", err)
	}
}

// TestDegradedCoverageCountsAllWarnings proves the warning status counts every
// warning fact: a degraded warning followed by informational warnings reports
// the full count, not the prefix before the first degraded code.
func TestDegradedCoverageCountsAllWarnings(t *testing.T) {
	t.Parallel()

	warning := func(code string) sdk.Fact {
		return sdk.Fact{Kind: oci.FactKindWarning, Payload: map[string]any{"warning_code": code}}
	}
	facts := []sdk.Fact{
		warning(oci.WarningUnsupportedReferrersAPI),
		warning("computed_digest"),
		warning("config_blob_read"),
	}
	partial, count := degradedCoverage(facts)
	if !partial {
		t.Fatal("degradedCoverage() partial = false, want true")
	}
	if count != len(facts) {
		t.Fatalf("degradedCoverage() count = %d, want %d", count, len(facts))
	}
}
