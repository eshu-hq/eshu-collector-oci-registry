// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestResolveTargetEnforcesScopeInSingleTargetMode proves single-target mode
// enforces claim-scope matching at config decode: a claim for another
// repository is rejected here instead of at the ScanClaim match.
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

// TestHandleRejectsMismatchedSingleTargetClaimTerminal proves end-to-end
// protection: a single-target activation serving a claim for another
// repository returns terminal invalid_config with no facts, so the wrong
// target is never scanned.
func TestHandleRejectsMismatchedSingleTargetClaimTerminal(t *testing.T) {
	t.Parallel()

	claim := handlerClaim()
	claim.Scope.ID = "oci-registry://ghcr.io/eshu-hq/other"
	result, err := handlerWith(fullHandlerClient(t)).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Contract:        Contract(),
		Config:          handlerTargetConfig(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal", result.State)
	}
	if len(result.Facts) != 0 {
		t.Fatal("terminal result carries facts")
	}
	if result.Statuses[0].FailureClass != FailureInvalidConfig {
		t.Fatalf("FailureClass = %q, want %q", result.Statuses[0].FailureClass, FailureInvalidConfig)
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
