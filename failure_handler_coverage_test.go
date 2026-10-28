// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-collector-oci-registry/internal/httpx"
	"github.com/eshu-hq/eshu-collector-oci-registry/internal/regfail"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/runtime"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestClassifyFailureCarriesProviderOperation proves an HTTP failure without
// its own operation token inherits the nested provider operation.
func TestClassifyFailureCarriesProviderOperation(t *testing.T) {
	t.Parallel()

	err := httpx.HTTPError{
		StatusCode: 500,
		Cause:      httpx.HTTPError{StatusCode: 500, Operation: "GetManifest"},
	}
	got := classifyFailure(context.Background(), err)
	if got.Class != regfail.RegistryFailureRetryable {
		t.Fatalf("Class = %q, want retryable", got.Class)
	}
	// Operation tokens are sanitized to lowercase by the provider failure.
	if got.Operation != "getmanifest" {
		t.Fatalf("Operation = %q, want the carried operation", got.Operation)
	}
	if got.Terminal {
		t.Fatal("Terminal = true, want retryable")
	}
}

// TestClassifyFailureMapsRegistryFailures proves the registry-failure branch:
// an empty class defaults to terminal, a named retryable class retries, and
// a rate-limited failure keeps its retry guidance.
func TestClassifyFailureMapsRegistryFailures(t *testing.T) {
	t.Parallel()

	terminal := classifyFailure(context.Background(), regfail.RegistryFailure{Cause: errors.New("boom")})
	if terminal.Class != regfail.RegistryFailureTerminal || !terminal.Terminal {
		t.Fatalf("terminal = %+v, want the default terminal failure", terminal)
	}
	retryable := classifyFailure(context.Background(), regfail.RegistryFailure{
		Class: regfail.RegistryFailureRetryable,
		Cause: errors.New("boom"),
	})
	if retryable.Class != regfail.RegistryFailureRetryable || retryable.Terminal {
		t.Fatalf("retryable = %+v, want a non-terminal retryable failure", retryable)
	}
	rateLimited := classifyFailure(context.Background(), regfail.RegistryFailure{
		Class: regfail.RegistryFailureRateLimited,
		Cause: errors.New("boom"),
	})
	if rateLimited.Terminal {
		t.Fatalf("rateLimited = %+v, want a non-terminal failure", rateLimited)
	}
	if got := retryAfterSeconds(rateLimited); got != 60 {
		t.Fatalf("retryAfterSeconds() = %d, want the rate-limit default 60", got)
	}
}

// TestClassifyFailureMapsTransportAndPlainErrors proves transient transport
// errors retry while plain errors fail terminal.
func TestClassifyFailureMapsTransportAndPlainErrors(t *testing.T) {
	t.Parallel()

	if got := classifyFailure(context.Background(), io.ErrUnexpectedEOF); got.Class != regfail.RegistryFailureRetryable {
		t.Fatalf("Class = %q, want retryable", got.Class)
	}
	if got := classifyFailure(context.Background(), errors.New("boom")); !got.Terminal {
		t.Fatalf("plain = %+v, want a terminal failure", got)
	}
	if got := retryAfterSeconds(Failure{RetryAfter: 1500 * time.Millisecond}); got != 2 {
		t.Fatalf("retryAfterSeconds() = %d, want the ceiling 2", got)
	}
	if got := retryAfterSeconds(Failure{}); got != 30 {
		t.Fatalf("retryAfterSeconds() = %d, want the default 30", got)
	}
	if got := latencyMillis(1500 * time.Microsecond); got != 1 {
		t.Fatalf("latencyMillis() = %d, want 1", got)
	}
	if got := latencyMillis(0); got != 0 {
		t.Fatalf("latencyMillis() = %d, want 0", got)
	}
}

// TestHandlerClockAndEnvDefaults proves the zero-value handler reads the
// process clock and environment.
func TestHandlerClockAndEnvDefaults(t *testing.T) {
	t.Setenv("ESHU_OCI_CLUSTER1_PROBE", "probe-value")

	var h Handler
	if h.now().IsZero() {
		t.Fatal("now() is zero, want the process clock")
	}
	if got := h.getenv("ESHU_OCI_CLUSTER1_PROBE"); got != "probe-value" {
		t.Fatalf("getenv() = %q, want the process value", got)
	}
}

// TestClampedDeadlineTimeoutBoundsWork proves the lease clamp: no deadline
// keeps the full budget, a spent lease stops work, and a near deadline
// clamps below the budget.
func TestClampedDeadlineTimeoutBoundsWork(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	budget := 5 * time.Minute
	if got, ok := clampedDeadlineTimeout(now, time.Time{}, budget); !ok || got != budget {
		t.Fatalf("zero deadline = %v, %v, want the full budget", got, ok)
	}
	if _, ok := clampedDeadlineTimeout(now, now.Add(-time.Second), budget); ok {
		t.Fatal("spent lease is bounded, want it to stop work")
	}
	got, ok := clampedDeadlineTimeout(now, now.Add(10*time.Second), budget)
	if !ok || got >= budget || got <= 0 {
		t.Fatalf("near deadline = %v, %v, want a positive clamp below budget", got, ok)
	}
}

// TestHandleRejectsBadFencingToken proves a non-integer fencing token fails
// terminal before any provider call.
func TestHandleRejectsBadFencingToken(t *testing.T) {
	t.Parallel()

	claim := handlerClaim()
	claim.FencingToken = "abc"
	result, err := handlerWith(fullHandlerClient(t)).Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Contract:        Contract(),
		Config:          handlerTargetConfig(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.State != sdk.ResultTerminal || result.Statuses[0].FailureClass != FailureInvalidConfig {
		t.Fatalf("result = %+v, want terminal invalid_config", result)
	}
}

// TestHandleServesWithNilClientFactory proves the zero-value factory path:
// with no injected client the handler builds the default factory, whose
// client-construction failure maps to a fact-free failure instead of a
// panic. ECR with an unloadable shared config fails inside client
// construction, so the test never touches the network. (A registry-host
// override cannot serve this purpose: the dockerhub adapter dials its
// pinned base URL regardless of the target registry.)
func TestHandleServesWithNilClientFactory(t *testing.T) {
	pinAWSEnv(t, "")
	// A directory where the SDK expects an INI file fails shared-config
	// loading deterministically.
	t.Setenv("AWS_CONFIG_FILE", t.TempDir())

	mapped, err := mapTarget(targetJSON{
		Provider:   "ecr",
		RegistryID: "123456789012",
		Region:     "us-east-1",
		Repository: "team/api",
		SourceURI:  "urn:test:ecr-factory",
	}, blankGetenv)
	if err != nil {
		t.Fatalf("mapTarget() error = %v", err)
	}
	scope, err := scopeIDFor(mapped)
	if err != nil {
		t.Fatalf("scopeIDFor() error = %v", err)
	}
	claim := handlerClaim()
	claim.Scope.ID = scope
	config := map[string]any{
		"provider":    "ecr",
		"registry_id": "123456789012",
		"region":      "us-east-1",
		"repository":  "team/api",
		"source_uri":  "urn:test:ecr-factory",
	}

	var handler Handler
	result, err := handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Contract:        Contract(),
		Config:          config,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(result.Facts) != 0 {
		t.Fatal("failure result carries facts")
	}
	if result.State != sdk.ResultRetryable && result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want a failure state", result.State)
	}
}

// TestCompleteResultFallsBackToStarted proves a scan generation without an
// observed timestamp commits under the claim start time.
func TestCompleteResultFallsBackToStarted(t *testing.T) {
	t.Parallel()

	var handler Handler
	started := time.Now().UTC()
	result := handler.completeResult(handlerClaim(), runtime.ScanResult{}, started, DefaultResourceUse())
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete", result.State)
	}
	if !result.Generation.ObservedAt.Equal(started) {
		t.Fatalf("ObservedAt = %v, want the claim start %v", result.Generation.ObservedAt, started)
	}
}

// TestHandlerEmitWritesLogLines proves the stderr diagnostic path encodes
// one JSON line per event.
func TestHandlerEmitWritesLogLines(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := Handler{Log: &buf}
	handler.emit(logLine{Event: "oci_registry.claim", Outcome: "failure"})
	line := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(line, "{") || !strings.Contains(line, "oci_registry.claim") {
		t.Fatalf("emit() wrote %q, want one JSON log line", line)
	}
}
