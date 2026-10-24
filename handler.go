// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/runtime"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// degradedWarningCodes are the in-band warning codes that mark a scan as
// degraded coverage: the registry hid the referrers API or the tag list
// exceeded the bounded window. Other warnings (computed digests, config-blob
// reads) are informational and keep the generation complete.
var degradedWarningCodes = map[string]struct{}{
	oci.WarningUnsupportedReferrersAPI: {},
	oci.WarningTagListTruncated:        {},
}

const (
	maxDeadlineMargin     = 2 * time.Second
	deadlineMarginDivisor = 20
)

// errClaimDeadline is the cancellation cause of a context that ended because
// the claim lease deadline (less its margin) was reached. It tells that stop
// apart from a host cancel and from the configured claim timeout.
var errClaimDeadline = errors.New("claim lease deadline reached")

// Handler runs one claim. The zero value is usable: it reads the process
// environment, the wall clock, and the real per-provider registry HTTP
// clients.
type Handler struct {
	// Getenv resolves credential environment variables; nil selects os.Getenv,
	// so the zero value reads the process environment.
	Getenv func(string) string
	// Now supplies the clock; nil selects time.Now.
	Now func() time.Time
	// NewClient builds registry clients; nil selects the provider factory.
	NewClient runtime.ClientFactory
	// Log receives one bounded JSON diagnostic line per claim; nil discards
	// it. The binary points it at stderr, which the extension host's process
	// runner does not surface (it keeps only a byte count), so the line helps
	// a local run and a test, not an operator in production.
	Log io.Writer
	// Limits bounds one claim's emission; the zero value selects defaults.
	Limits ResourceUse
}

func (h Handler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h Handler) getenv(key string) string {
	if h.Getenv != nil {
		return h.Getenv(key)
	}
	return os.Getenv(key)
}

func (h Handler) limits() ResourceUse {
	return h.Limits.withDefaults()
}

// Handle collects the repository named by the claim and returns the SDK
// result the host validates and commits. It never returns facts together with
// a failure state, never lets a credential into a result, and echoes the
// claim unchanged so the host's identity check passes. A host cancellation
// returns an error instead of a result so nothing commits for a dead claim.
func (h Handler) Handle(ctx context.Context, request Request) (sdk.Result, error) {
	started := h.now()
	claim := request.Claim
	limits := h.limits()
	target, err := resolveTarget(request.Config, claim.Scope.ID, h.getenv)
	if err != nil {
		h.logConfigFailure(claim, err)
		return budgetResult(claim, started, FailureInvalidConfig), nil
	}
	token, err := fencingToken(claim.FencingToken)
	if err != nil {
		h.logConfigFailure(claim, err)
		return budgetResult(claim, started, FailureInvalidConfig), nil
	}
	runCtx, cancel, ok := claimContext(ctx, claim, started, limits)
	defer cancel()
	if !ok {
		h.logFailure(claim, FailureClaimDeadline, "", 0)
		return failureResult(claim, started, Failure{Class: FailureClaimDeadline}, 0), nil
	}
	factory := h.NewClient
	if factory == nil {
		factory = providerFactory{}
	}
	source := runtime.Source{
		Config: runtime.Config{
			CollectorInstanceID: claim.InstanceID,
			Targets:             []runtime.TargetConfig{target},
		},
		ClientFactory: factory,
		Clock:         h.Now,
	}
	collected, matched, err := source.ScanClaim(runCtx, claim.Scope.ID, claim.GenerationID, token)
	if err != nil || stoppedAtClaimDeadline(runCtx) {
		if runCtx.Err() != nil && !stoppedAtClaimDeadline(runCtx) && ctx.Err() != nil {
			return sdk.Result{}, ctx.Err()
		}
		failure := Failure{}
		if err != nil {
			failure = classifyFailure(runCtx, err)
		}
		if stoppedAtClaimDeadline(runCtx) && !failure.Terminal {
			failure = Failure{Class: FailureClaimDeadline}
		}
		latency := h.now().Sub(started)
		h.logFailure(claim, failure.Class, failure.Operation, failure.HTTPStatus)
		return failureResult(claim, started, failure, latency), nil
	}
	if !matched {
		h.logConfigFailure(claim, fmt.Errorf("no configured target matches scope_id %q", claim.Scope.ID))
		return budgetResult(claim, started, FailureInvalidConfig), nil
	}
	return h.completeResult(claim, collected, started, limits), nil
}

func (h Handler) completeResult(claim sdk.Claim, collected runtime.ScanResult, started time.Time, limits ResourceUse) sdk.Result {
	observedAt := collected.Generation.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = started
	}
	facts := collected.Facts
	if class := checkBudgets(facts, limits); class != "" {
		h.logFailure(claim, class, "", 0)
		return budgetResult(claim, started, class)
	}
	partial, warnings := degradedCoverage(facts)
	result := sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultComplete,
		Claim:           claim,
		Generation: sdk.Generation{
			ID:              claim.GenerationID,
			ObservedAt:      observedAt,
			FreshnessHint:   collected.Generation.FreshnessHint,
			TrustedDeletion: collected.Generation.TrustedDeletion,
		},
		Facts: facts,
		Statuses: []sdk.Status{{
			Class:           sdk.StatusComplete,
			FactCount:       len(facts),
			SourceLatencyMS: latencyMillis(h.now().Sub(started)),
		}},
	}
	if partial {
		// Referrer-API and tag-window gaps are committed as warning facts,
		// so the generation stays useful; the partial state tells the
		// operator that some coverage is degraded.
		result.State = sdk.ResultPartial
		result.Statuses = append(result.Statuses, sdk.Status{
			Class: sdk.StatusWarning, Partial: true, WarningCount: warnings,
		})
	}
	if encoded, err := json.Marshal(result); err != nil || len(encoded) > limits.MaxResultBytes {
		h.logFailure(claim, FailureResultBudget, "", 0)
		return budgetResult(claim, started, FailureResultBudget)
	}
	h.logSuccess(claim, result)
	return result
}

// degradedCoverage reports whether the scan carries degraded coverage: any
// warning fact whose code marks hidden referrers or a cut tag window.
func degradedCoverage(facts []sdk.Fact) (bool, int) {
	warnings := 0
	for _, fact := range facts {
		if fact.Kind != oci.FactKindWarning {
			continue
		}
		warnings++
		if code, ok := fact.Payload["warning_code"].(string); ok {
			if _, degraded := degradedWarningCodes[code]; degraded {
				return true, warnings
			}
		}
	}
	return false, warnings
}

// checkBudgets returns the failure class of the first exhausted bound, or "".
func checkBudgets(facts []sdk.Fact, limits ResourceUse) string {
	if len(facts) > limits.MaxRecordsPerClaim {
		return FailureRecordBudget
	}
	for _, fact := range facts {
		encoded, err := json.Marshal(fact.Payload)
		if err != nil || len(encoded) > limits.MaxPayloadBytes {
			return FailurePayloadBudget
		}
	}
	return ""
}

// clampedDeadlineTimeout returns how long the collector may work on a claim:
// the time left until deadline minus a safety margin of min(2s, 5% of the time
// left). The host kills the process at the deadline, so the collector stops
// starting provider calls a little earlier and ends with a retryable result
// of its own instead of being killed mid-request.
func clampedDeadlineTimeout(now, deadline time.Time, budget time.Duration) (time.Duration, bool) {
	if deadline.IsZero() {
		return budget, true
	}
	remaining := deadline.Sub(now)
	if remaining <= 0 {
		return 0, false
	}
	margin := min(maxDeadlineMargin, remaining/deadlineMarginDivisor)
	if timeout := remaining - margin; timeout < budget {
		return timeout, true
	}
	return budget, true
}

// claimContext bounds the collection by the claim lease deadline (less its
// margin) and the configured claim timeout, whichever is shorter. A context
// that ends at the lease bound carries errClaimDeadline as its cause. ok is
// false when the lease is already spent.
func claimContext(
	ctx context.Context,
	claim sdk.Claim,
	now time.Time,
	limits ResourceUse,
) (context.Context, context.CancelFunc, bool) {
	budget := time.Duration(limits.ClaimTimeoutSeconds) * time.Second
	var cause error
	timeout := budget
	if !claim.Deadline.IsZero() {
		deadline := claim.Deadline.UTC()
		if !deadline.After(now) {
			return ctx, func() {}, false
		}
		var bounded bool
		timeout, bounded = clampedDeadlineTimeout(now, deadline, budget)
		if !bounded {
			return ctx, func() {}, false
		}
		if timeout < budget {
			cause = errClaimDeadline
		}
	}
	runCtx, cancel := context.WithTimeoutCause(ctx, timeout, cause)
	return runCtx, cancel, true
}

// stoppedAtClaimDeadline reports whether ctx ended because the claim lease
// deadline was reached, as opposed to a host cancel or the claim timeout.
func stoppedAtClaimDeadline(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errClaimDeadline)
}

// logLine is the bounded diagnostic record written to stderr. Every field is
// a counter or a fixed-vocabulary token; none carries a URL, a provider body,
// or a secret.
type logLine struct {
	Event        string `json:"event"`
	Outcome      string `json:"outcome"`
	ScopeID      string `json:"scope_id"`
	GenerationID string `json:"generation_id"`
	WorkItemID   string `json:"work_item_id"`
	Attempt      int    `json:"attempt"`
	FailureClass string `json:"failure_class,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Operation    string `json:"operation,omitempty"`
	HTTPStatus   int    `json:"http_status,omitempty"`
	Facts        int    `json:"facts,omitempty"`
}

func (h Handler) logFailure(claim sdk.Claim, class string, operation string, status int) {
	h.emit(logLine{
		Event: "oci_registry.claim", Outcome: "failure", ScopeID: claim.Scope.ID, GenerationID: claim.GenerationID,
		WorkItemID: claim.WorkItemID, Attempt: claim.Attempt, FailureClass: class, Operation: operation,
		HTTPStatus: status,
	})
}

func (h Handler) logConfigFailure(claim sdk.Claim, err error) {
	h.emit(logLine{
		Event: "oci_registry.claim", Outcome: "failure", ScopeID: claim.Scope.ID, GenerationID: claim.GenerationID,
		WorkItemID: claim.WorkItemID, Attempt: claim.Attempt, FailureClass: FailureInvalidConfig,
		Detail: err.Error(),
	})
}

func (h Handler) logSuccess(claim sdk.Claim, result sdk.Result) {
	h.emit(logLine{
		Event: "oci_registry.claim", Outcome: string(result.State), ScopeID: claim.Scope.ID, GenerationID: claim.GenerationID,
		WorkItemID: claim.WorkItemID, Attempt: claim.Attempt, Facts: len(result.Facts),
	})
}

func (h Handler) emit(line logLine) {
	if h.Log == nil {
		return
	}
	encoded, err := json.Marshal(line)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(h.Log, "%s\n", strings.TrimSpace(string(encoded)))
}
