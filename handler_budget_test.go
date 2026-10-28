// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-collector-oci-registry/internal/regfail"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestHandleEnforcesConfigLimits proves the activation config limits knob is
// wired to enforcement: a record bound below the scan's emission fails
// terminal with the record-budget class and no facts.
func TestHandleEnforcesConfigLimits(t *testing.T) {
	t.Parallel()

	config := handlerTargetConfig()
	config["limits"] = map[string]any{"max_records_per_claim": 1}
	result, err := handlerWith(fullHandlerClient(t)).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          config,
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
	if result.Statuses[0].FailureClass != FailureRecordBudget {
		t.Fatalf("FailureClass = %q, want %q", result.Statuses[0].FailureClass, FailureRecordBudget)
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

// TestHandleFillsUnsetConfigLimitsFromDefaults proves a partial limits object
// keeps working bounds: fields the activation leaves unset fall back to the
// defaults instead of zeroing enforcement.
func TestHandleFillsUnsetConfigLimitsFromDefaults(t *testing.T) {
	t.Parallel()

	config := handlerTargetConfig()
	config["limits"] = map[string]any{"claim_timeout_seconds": 600}
	result, err := handlerWith(fullHandlerClient(t)).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          config,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete", result.State)
	}
	if len(result.Facts) == 0 {
		t.Fatal("complete result carries no facts")
	}
}

// TestHandleEnforcesConfigPayloadBudget proves a per-fact payload bound from
// config is enforced: a bound below every emitted payload fails terminal
// with the payload-budget class and no facts.
func TestHandleEnforcesConfigPayloadBudget(t *testing.T) {
	t.Parallel()

	config := handlerTargetConfig()
	config["limits"] = map[string]any{"max_payload_bytes": 1}
	result, err := handlerWith(fullHandlerClient(t)).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          config,
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
	if result.Statuses[0].FailureClass != FailurePayloadBudget {
		t.Fatalf("FailureClass = %q, want %q", result.Statuses[0].FailureClass, FailurePayloadBudget)
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

// TestHandleEnforcesConfigResultBudget proves an encoded-result bound from
// config is enforced: a bound below the result encoding fails terminal
// with the result-budget class and no facts.
func TestHandleEnforcesConfigResultBudget(t *testing.T) {
	t.Parallel()

	config := handlerTargetConfig()
	config["limits"] = map[string]any{"max_result_bytes": 1}
	result, err := handlerWith(fullHandlerClient(t)).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          config,
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
	if result.Statuses[0].FailureClass != FailureResultBudget {
		t.Fatalf("FailureClass = %q, want %q", result.Statuses[0].FailureClass, FailureResultBudget)
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

// TestHandleEnforcesConfigTimeout proves the configured claim timeout value
// fires: with a provider that stalls until cancellation, a one-second config
// bound ends the scan in about a second — far below the 300s handler default
// and the one-hour claim deadline, so an ignored config value (300s+) cannot
// pass the elapsed bound. The stalled scan reports terminal
// registry_canceled with no facts.
func TestHandleEnforcesConfigTimeout(t *testing.T) {
	t.Parallel()

	config := handlerTargetConfig()
	config["limits"] = map[string]any{"claim_timeout_seconds": 1}
	client := blockingRegistryClient{}
	started := time.Now()
	result, err := handlerWith(client).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          config,
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal", result.State)
	}
	if result.Statuses[0].FailureClass != regfail.RegistryFailureCanceled {
		t.Fatalf("FailureClass = %q, want %q", result.Statuses[0].FailureClass, regfail.RegistryFailureCanceled)
	}
	if len(result.Facts) != 0 {
		t.Fatal("timed-out scan carries facts")
	}
	if elapsed > 30*time.Second {
		t.Fatalf("elapsed = %v, want the 1s config timeout to end the scan (handler default is 300s)", elapsed)
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}
