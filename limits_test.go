// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
)

// TestLimitsWithDefaultsClampsToCeilings proves the ceilings named in
// limits.go are enforced: an over-ceiling activation value resolves to the
// ceiling instead of sizing per-claim memory/CPU without limit.
func TestLimitsWithDefaultsClampsToCeilings(t *testing.T) {
	t.Parallel()

	over := ResourceUse{
		MaxRecordsPerClaim:  5_000_000,
		MaxPayloadBytes:     64 << 20,
		MaxResultBytes:      512 << 20,
		ClaimTimeoutSeconds: 7200,
	}
	got := over.withDefaults()
	want := ResourceUse{
		MaxRecordsPerClaim:  maxRecordsCeiling,
		MaxPayloadBytes:     maxPayloadCeiling,
		MaxResultBytes:      maxResultCeiling,
		ClaimTimeoutSeconds: maxTimeoutCeiling,
	}
	if got != want {
		t.Fatalf("withDefaults(over-ceiling) = %+v, want %+v", got, want)
	}
}

// TestLimitsOverlayClampsToCeilings proves the merged activation value (the
// target's own bounds with the envelope filling the gaps) is clamped too: a
// large envelope value cannot escape through the overlay path.
func TestLimitsOverlayClampsToCeilings(t *testing.T) {
	t.Parallel()

	target := ResourceUse{MaxRecordsPerClaim: 5_000_000}
	envelope := ResourceUse{
		MaxRecordsPerClaim:  50_000,
		MaxPayloadBytes:     64 << 20,
		MaxResultBytes:      3 << 20,
		ClaimTimeoutSeconds: 300,
	}
	got := target.overlay(envelope)
	if got.MaxRecordsPerClaim != maxRecordsCeiling {
		t.Fatalf("overlay MaxRecordsPerClaim = %d, want ceiling %d", got.MaxRecordsPerClaim, maxRecordsCeiling)
	}
	if got.MaxPayloadBytes != maxPayloadCeiling {
		t.Fatalf("overlay MaxPayloadBytes = %d, want ceiling %d", got.MaxPayloadBytes, maxPayloadCeiling)
	}
}

// TestLimitsAtCeilingPassThrough proves values at exactly the ceiling are
// kept: the clamp only cuts values above it.
func TestLimitsAtCeilingPassThrough(t *testing.T) {
	t.Parallel()

	at := ResourceUse{
		MaxRecordsPerClaim:  maxRecordsCeiling,
		MaxPayloadBytes:     maxPayloadCeiling,
		MaxResultBytes:      maxResultCeiling,
		ClaimTimeoutSeconds: maxTimeoutCeiling,
	}
	if got := at.withDefaults(); got != at {
		t.Fatalf("withDefaults(at-ceiling) = %+v, want %+v", got, at)
	}
	if got := at.overlay(DefaultResourceUse()); got != at {
		t.Fatalf("overlay(at-ceiling) = %+v, want %+v", got, at)
	}
}
