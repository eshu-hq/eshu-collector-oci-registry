// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-collector-oci-registry/internal/httpx"
	"github.com/eshu-hq/eshu-collector-oci-registry/internal/regfail"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/distribution"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/runtime"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// stubRegistryClient serves canned Distribution responses without a socket.
type stubRegistryClient struct {
	tags         []string
	incomplete   bool
	manifest     distribution.ManifestResponse
	manifestErr  error
	referrers    distribution.ReferrersResponse
	referrersErr error
	pingErr      error
}

func (s *stubRegistryClient) Ping(context.Context) error { return s.pingErr }

func (s *stubRegistryClient) ListTags(_ context.Context, _ string, _ int) (distribution.TagListResponse, error) {
	return distribution.TagListResponse{Tags: append([]string(nil), s.tags...), Complete: !s.incomplete}, nil
}

func (s *stubRegistryClient) GetManifest(_ context.Context, _, _ string) (distribution.ManifestResponse, error) {
	if s.manifestErr != nil {
		return distribution.ManifestResponse{}, s.manifestErr
	}
	return s.manifest, nil
}

func (s *stubRegistryClient) GetBlob(context.Context, string, string) (distribution.BlobResponse, error) {
	return distribution.BlobResponse{}, nil
}

func (s *stubRegistryClient) ListReferrers(_ context.Context, _, _ string) (distribution.ReferrersResponse, error) {
	if s.referrersErr != nil {
		return distribution.ReferrersResponse{}, s.referrersErr
	}
	return s.referrers, nil
}

func handlerManifest(t *testing.T) distribution.ManifestResponse {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     oci.MediaTypeOCIImageManifest,
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    parityConfigDigest,
			"size":      256,
		},
		"layers": []map[string]any{{
			"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
			"digest":    parityLayerDigest,
			"size":      1024,
		}},
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return distribution.ManifestResponse{
		Digest:    parityDigest,
		MediaType: oci.MediaTypeOCIImageManifest,
		Body:      body,
		SizeBytes: int64(len(body)),
	}
}

func handlerClaim() sdk.Claim {
	return sdk.Claim{
		ComponentID:   ComponentID,
		InstanceID:    "oci-handler-test",
		CollectorKind: CollectorKind,
		SourceSystem:  SourceSystem,
		Scope:         sdk.Scope{ID: "oci-registry://registry-1.docker.io/library/busybox", Kind: "container_registry_repository"},
		SourceRunID:   "run-1",
		GenerationID:  "generation-handler-1",
		WorkItemID:    "work-1",
		FencingToken:  "7",
		Attempt:       1,
		Deadline:      time.Now().Add(time.Hour).UTC(),
		ConfigHandle:  "component-config:oci-registry",
	}
}

func handlerTargetConfig() map[string]any {
	return map[string]any{
		"provider":   "dockerhub",
		"registry":   "registry-1.docker.io",
		"repository": "library/busybox",
		"tag_limit":  10,
		"visibility": "public",
		"auth_mode":  "anonymous",
		"source_uri": "https://registry-1.docker.io/v2/library/busybox",
	}
}

func handlerWith(client runtime.RegistryClient) Handler {
	return Handler{
		Now: func() time.Time { return parityObservedAt },
		NewClient: runtime.ClientFactoryFunc(func(context.Context, runtime.TargetConfig) (runtime.RegistryClient, error) {
			return client, nil
		}),
	}
}

func fullHandlerClient(t *testing.T) *stubRegistryClient {
	t.Helper()
	return &stubRegistryClient{
		tags:     []string{"latest"},
		manifest: handlerManifest(t),
		referrers: distribution.ReferrersResponse{Referrers: []oci.Descriptor{{
			Digest:       parityReferrer,
			MediaType:    oci.MediaTypeOCIImageManifest,
			SizeBytes:    128,
			ArtifactType: "application/vnd.in-toto+json",
		}}},
	}
}

// TestHandleServesAClaimComplete proves the happy path: the handler scans the
// claimed scope, echoes the claim and generation, emits facts that validate
// against the contract, and reports complete.
func TestHandleServesAClaimComplete(t *testing.T) {
	t.Parallel()

	claim := handlerClaim()
	handler := handlerWith(fullHandlerClient(t))
	result, err := handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Contract:        Contract(),
		Config:          handlerTargetConfig(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete", result.State)
	}
	if result.Claim != claim {
		t.Fatalf("Claim was not echoed: %+v", result.Claim)
	}
	if result.Generation.ID != claim.GenerationID {
		t.Fatalf("Generation.ID = %q, want %q", result.Generation.ID, claim.GenerationID)
	}
	if len(result.Facts) == 0 {
		t.Fatal("complete result carries no facts")
	}
	for _, want := range []string{FactKindRepository, FactKindTagObservation, FactKindManifest, FactKindDescriptor, FactKindReferrer} {
		found := false
		for _, fact := range result.Facts {
			if fact.Kind == want {
				found = true
				if fact.SourceRef.ScopeID != claim.Scope.ID || fact.SourceRef.GenerationID != claim.GenerationID {
					t.Fatalf("fact %q does not carry the claim boundary", want)
				}
			}
		}
		if !found {
			t.Fatalf("result carries no %q fact", want)
		}
	}
	report, err := sdk.NewValidator(Contract()).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
	if report.FactCount != len(result.Facts) {
		t.Fatalf("FactCount = %d, want %d", report.FactCount, len(result.Facts))
	}
}

// TestHandleMarksDegradedCoveragePartial proves the SHIM_MAP partial mapping:
// an unsupported Referrers API still commits its warning fact, but the result
// is partial with a warning status.
func TestHandleMarksDegradedCoveragePartial(t *testing.T) {
	t.Parallel()

	client := fullHandlerClient(t)
	client.referrers = distribution.ReferrersResponse{}
	client.referrersErr = errors.New("referrers API not supported by this registry")
	result, err := handlerWith(client).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          handlerTargetConfig(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.State != sdk.ResultPartial {
		t.Fatalf("State = %q, want partial", result.State)
	}
	var warnings int
	for _, status := range result.Statuses {
		if status.Class == sdk.StatusWarning && status.Partial {
			warnings = status.WarningCount
		}
	}
	if warnings == 0 {
		t.Fatalf("partial result has no warning status: %+v", result.Statuses)
	}
	foundWarning := false
	for _, fact := range result.Facts {
		if fact.Kind == FactKindWarning {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatal("partial result carries no warning fact")
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

// TestHandleRejectsBadConfigTerminal proves configuration failures fail
// terminal without facts: an unsupported provider can never succeed on retry.
func TestHandleRejectsBadConfigTerminal(t *testing.T) {
	t.Parallel()

	config := handlerTargetConfig()
	config["provider"] = "example"
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
	if result.Statuses[0].FailureClass != FailureInvalidConfig {
		t.Fatalf("FailureClass = %q, want %q", result.Statuses[0].FailureClass, FailureInvalidConfig)
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

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

// TestHandleRejectsSpentLeaseRetryable proves an already-expired lease fails
// retryable with the claim-deadline class and no facts, in a shape the host
// validator accepts.
func TestHandleRejectsSpentLeaseRetryable(t *testing.T) {
	t.Parallel()

	claim := handlerClaim()
	claim.Deadline = parityObservedAt.Add(-time.Minute).UTC()
	result, err := handlerWith(fullHandlerClient(t)).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Contract:        Contract(),
		Config:          handlerTargetConfig(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.State != sdk.ResultRetryable || result.Statuses[0].FailureClass != FailureClaimDeadline {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Facts) != 0 {
		t.Fatal("retryable result carries facts")
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

// TestHandleMapsRegistryFailuresToRetryableOrTerminal proves the host-facing
// failure mapping: denials fail terminal, retryable statuses retry.
func TestHandleMapsRegistryFailuresToRetryableOrTerminal(t *testing.T) {
	t.Parallel()

	denied := &stubRegistryClient{pingErr: regfail.RegistryHTTPFailure("oci", "", "ping", 401, httpx.HTTPError{
		Provider: "oci", StatusCode: 401, Message: "Unauthorized", Operation: "ping",
	})}
	result, err := handlerWith(denied).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          handlerTargetConfig(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("denied State = %q, want terminal", result.State)
	}
	if result.Statuses[0].FailureClass != regfail.RegistryFailureAuthDenied {
		t.Fatalf("FailureClass = %q, want %q", result.Statuses[0].FailureClass, regfail.RegistryFailureAuthDenied)
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult(denied) error = %v", err)
	}

	unavailable := &stubRegistryClient{pingErr: regfail.RegistryHTTPFailure("oci", "", "ping", 500, httpx.HTTPError{
		Provider: "oci", StatusCode: 500, Message: "Internal Server Error", Operation: "ping",
	})}
	result, err = handlerWith(unavailable).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          handlerTargetConfig(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.State != sdk.ResultRetryable {
		t.Fatalf("unavailable State = %q, want retryable", result.State)
	}
	if result.Statuses[0].RetryAfterSeconds <= 0 {
		t.Fatal("retryable result has no retry guidance")
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult(unavailable) error = %v", err)
	}
}

// TestHandleReturnsNoResultOnCancellation proves a dead claim commits
// nothing: host cancellation returns an error instead of a result.
func TestHandleReturnsNoResultOnCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handlerWith(fullHandlerClient(t)).Handle(ctx, Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          handlerTargetConfig(),
	}); err == nil {
		t.Fatal("Handle(canceled) error = nil, want no result")
	}
}
