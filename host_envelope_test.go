// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestResolveTargetAcceptsTopLevelLimitsEnvelope is the issue #9 repro: the
// component-extension host forwards the activation config subtree verbatim,
// so a {"targets":[...]} activation whose config carries a top-level limits
// object must resolve instead of failing terminal invalid_config.
func TestResolveTargetAcceptsTopLevelLimitsEnvelope(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"targets": []any{validTargetConfig()},
		"limits": map[string]any{
			"max_records_per_claim": 100,
			"max_payload_bytes":     2048,
			"max_result_bytes":      4096,
			"claim_timeout_seconds": 60,
		},
	}
	_, limits, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveTarget(envelope) error = %v, want the host limits envelope accepted", err)
	}
	want := ResourceUse{
		MaxRecordsPerClaim:  100,
		MaxPayloadBytes:     2048,
		MaxResultBytes:      4096,
		ClaimTimeoutSeconds: 60,
	}
	if limits != want {
		t.Fatalf("limits = %+v, want %+v (top-level budgets mapped onto the target)", limits, want)
	}
}

// TestResolveTargetAcceptsTopLevelLimitsCamelCase proves the host's camelCase
// spelling of the same four budget keys decodes identically: the host sends
// claimTimeoutSeconds/maxPayloadBytes/maxRecordsPerClaim/maxResultBytes.
func TestResolveTargetAcceptsTopLevelLimitsCamelCase(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"targets": []any{validTargetConfig()},
		"limits": map[string]any{
			"maxRecordsPerClaim":  100,
			"maxPayloadBytes":     2048,
			"maxResultBytes":      4096,
			"claimTimeoutSeconds": 60,
		},
	}
	_, limits, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveTarget(camelCase envelope) error = %v", err)
	}
	want := ResourceUse{
		MaxRecordsPerClaim:  100,
		MaxPayloadBytes:     2048,
		MaxResultBytes:      4096,
		ClaimTimeoutSeconds: 60,
	}
	if limits != want {
		t.Fatalf("limits = %+v, want %+v", limits, want)
	}
}

// TestResolveTargetPrefersPerTargetLimitsOverTopLevel proves precedence: the
// matched target's own bounds win where set and the top-level envelope fills
// only the gaps, before the handler bounds and built-in defaults.
func TestResolveTargetPrefersPerTargetLimitsOverTopLevel(t *testing.T) {
	t.Parallel()

	target := validTargetConfig()
	target["limits"] = map[string]any{"max_records_per_claim": 111}
	config := map[string]any{
		"targets": []any{target},
		"limits": map[string]any{
			"max_records_per_claim": 100,
			"max_payload_bytes":     2048,
			"max_result_bytes":      4096,
			"claim_timeout_seconds": 60,
		},
	}
	_, limits, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveTarget() error = %v", err)
	}
	want := ResourceUse{
		MaxRecordsPerClaim:  111,
		MaxPayloadBytes:     2048,
		MaxResultBytes:      4096,
		ClaimTimeoutSeconds: 60,
	}
	if limits != want {
		t.Fatalf("limits = %+v, want %+v (per-target wins, envelope fills gaps)", limits, want)
	}
}

// TestResolveTargetIgnoresHostMintedToken proves the host-minted token object
// the component-extension host injects per claim when a token provider is set
// is accepted and ignored: it never fails decode and never reaches an error.
func TestResolveTargetIgnoresHostMintedToken(t *testing.T) {
	t.Parallel()

	token := map[string]any{"token": "minted-per-claim", "scope_id": "oci-registry://registry-1.docker.io/library/busybox"}
	envelope := map[string]any{
		"targets":               []any{validTargetConfig()},
		"host_github_app_token": token,
	}
	if _, _, err := resolveTarget(envelope, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" }); err != nil {
		t.Fatalf("resolveTarget(envelope+token) error = %v, want the host token key ignored", err)
	}

	single := validTargetConfig()
	single["host_github_app_token"] = token
	if _, _, err := resolveTarget(single, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" }); err != nil {
		t.Fatalf("resolveTarget(single+token) error = %v, want the host token key ignored", err)
	}
}

// TestResolveTargetRejectsUnknownEnvelopeKey proves the envelope stays
// fail-closed: a genuinely unknown top-level key is still rejected without
// naming it, because an operator key can be a pasted credential.
func TestResolveTargetRejectsUnknownEnvelopeKey(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"targets":   []any{validTargetConfig()},
		"bogus_key": 1,
	}
	_, _, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
	if err == nil {
		t.Fatal("resolveTarget() error = nil, want unknown-field rejection")
	}
	if strings.Contains(err.Error(), "bogus_key") {
		t.Fatalf("rejection echoes the key: %v", err)
	}
}

// TestHandleServesClaimWithHostEnvelope proves the end-to-end issue #9 repro:
// an SDK request whose config carries the host limits envelope serves the
// claim instead of returning terminal invalid_config with zero source
// latency.
func TestHandleServesClaimWithHostEnvelope(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"targets": []any{handlerTargetConfig()},
		"limits": map[string]any{
			"claimTimeoutSeconds": 60,
			"maxPayloadBytes":     1048576,
			"maxRecordsPerClaim":  50000,
			"maxResultBytes":      50331648,
		},
		"host_github_app_token": map[string]any{
			"token":    "minted-per-claim",
			"scope_id": "oci-registry://registry-1.docker.io/library/busybox",
		},
	}
	result, err := handlerWith(fullHandlerClient(t)).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          config,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.State == sdk.ResultTerminal {
		t.Fatalf("State = terminal %+v, want the host envelope served, not invalid_config", result.Statuses)
	}
	if len(result.Facts) == 0 {
		t.Fatal("envelope result carries no facts")
	}
}

// TestResolveTargetAcceptsPerTargetCamelCaseLimits proves the camelCase
// tolerance also covers a target's own limits object: the host forwards the
// subtree verbatim, so an operator camelCase spelling there must not fail.
func TestResolveTargetAcceptsPerTargetCamelCaseLimits(t *testing.T) {
	t.Parallel()

	config := validTargetConfig()
	config["limits"] = map[string]any{"maxRecordsPerClaim": 100}
	_, limits, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveTarget() error = %v", err)
	}
	if limits.MaxRecordsPerClaim != 100 {
		t.Fatalf("limits.MaxRecordsPerClaim = %d, want 100", limits.MaxRecordsPerClaim)
	}
}

// TestResolveTargetRejectsUnknownLimitsField proves the limits object stays
// fail-closed in both spellings: an unknown bound is rejected without naming
// it, because a misplaced credential key must never be echoed.
func TestResolveTargetRejectsUnknownLimitsField(t *testing.T) {
	t.Parallel()

	for name, limits := range map[string]map[string]any{
		"top-level":         {"max_records_per_claim": 100, "token": "SECRET"},
		"per-target":        {"maxRecordsPerClaim": 100, "token": "SECRET"},
		"top-level-unknown": {"mex_records_per_claim": 100},
	} {
		config := map[string]any{"targets": []any{validTargetConfig()}}
		if name == "per-target" {
			target := validTargetConfig()
			target["limits"] = limits
			config["targets"] = []any{target}
		} else {
			config["limits"] = limits
		}
		_, _, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
		if err == nil {
			t.Fatalf("%s: resolveTarget() error = nil, want unknown-field rejection", name)
		}
		if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "mex_") {
			t.Fatalf("%s: rejection echoes the key or value: %v", name, err)
		}
	}
}

// TestResolveTargetRejectsNonObjectTopLevelLimits proves a top-level limits
// value that is not an object fails bounded without echoing the value.
func TestResolveTargetRejectsNonObjectTopLevelLimits(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"targets": []any{validTargetConfig()},
		"limits":  "SECRET",
	}
	_, _, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
	if err == nil {
		t.Fatal("resolveTarget() error = nil, want limits type rejection")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("rejection echoes the value: %v", err)
	}
}
