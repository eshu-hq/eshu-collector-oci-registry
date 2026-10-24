// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/eshu-hq/eshu-collector-oci-registry/internal/httpx"
	"github.com/eshu-hq/eshu-collector-oci-registry/internal/regfail"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

const (
	// FailureInvalidConfig marks an activation config the collector cannot run.
	FailureInvalidConfig = "invalid_config"
	// FailureClaimDeadline marks a claim whose lease deadline has passed.
	FailureClaimDeadline = "claim_deadline_exceeded"
	// FailureRecordBudget marks a claim past its fact-count bound.
	FailureRecordBudget = "record_budget_exceeded"
	// FailurePayloadBudget marks a fact past its payload-size bound.
	FailurePayloadBudget = "payload_budget_exceeded"
	// FailureResultBudget marks an encoded result past its size bound.
	FailureResultBudget = "result_budget_exceeded"

	defaultRetryAfterSeconds          = 30
	defaultRateLimitRetryAfterSeconds = 60
)

// Failure describes one bounded claim failure: the class and operation tokens
// are fixed vocabularies and never carry URLs, bodies, or credentials. Classes
// reuse the core registry failure vocabulary so host status surfaces stay
// consistent between the in-tree and extracted collectors.
type Failure struct {
	Class      string
	Operation  string
	HTTPStatus int
	Terminal   bool
	RetryAfter time.Duration
	Stage      string
}

// classifyFailure maps a scan error to its bounded failure. Auth denials,
// missing repositories, and other non-retryable statuses fail terminal;
// transient transport errors, retryable statuses, and rate limiting retry.
func classifyFailure(ctx context.Context, err error) Failure {
	var httpErr httpx.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode >= 100 && httpErr.StatusCode <= 599 {
		class := regfail.RegistryFailureClassForHTTPStatus(httpErr.StatusCode)
		failure := Failure{Class: class, HTTPStatus: httpErr.StatusCode}
		switch class {
		case regfail.RegistryFailureRetryable, regfail.RegistryFailureRateLimited:
			failure.RetryAfter = httpErr.RetryAfterDelay()
		default:
			failure.Terminal = true
		}
		if operation := httpErr.Operation; operation != "" {
			failure.Operation = operation
		}
		if unwrapped := errors.Unwrap(httpErr); unwrapped != nil {
			provider := httpx.ClassifyProviderFailure(ComponentID, unwrapped, ociStatusPolicy, httpx.FailureRetryable)
			if provider.FailureOperation() != "" && failure.Operation == "" {
				failure.Operation = provider.FailureOperation()
			}
		}
		return failure
	}
	var registryErr regfail.RegistryFailure
	if errors.As(err, &registryErr) {
		class := registryErr.FailureClass()
		if class == "" {
			class = regfail.RegistryFailureTerminal
		}
		// The distribution client stamps the bounded endpoint token and any
		// provider retry guidance on the nested HTTP error.
		var nested httpx.HTTPError
		_ = errors.As(err, &nested)
		failure := Failure{Class: class, Operation: nested.Operation}
		if nested.RetryAfterDelay() > 0 {
			failure.RetryAfter = nested.RetryAfterDelay()
		}
		switch class {
		case regfail.RegistryFailureRetryable, regfail.RegistryFailureRateLimited:
		default:
			failure.Terminal = true
		}
		return failure
	}
	if httpx.IsTransientTransportError(ctx, err) {
		return Failure{Class: regfail.RegistryFailureRetryable}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Failure{Class: regfail.RegistryFailureCanceled, Terminal: true}
	}
	return Failure{Class: regfail.RegistryFailureTerminal, Terminal: true}
}

// ociStatusPolicy maps HTTP statuses to the OCI failure vocabulary. Auth
// denials and missing repositories fail terminal; retryable statuses and rate
// limiting retry.
var ociStatusPolicy = httpx.StatusPolicy{
	AuthDeniedClass: httpx.FailureAuthDenied,
	NotFoundClass:   httpx.FailureNotFound,
}

// failureResult builds the SDK result for a failed claim: terminal when
// retrying cannot help, retryable otherwise. Neither carries facts.
func failureResult(claim sdk.Claim, observedAt time.Time, failure Failure, latency time.Duration) sdk.Result {
	state := sdk.ResultRetryable
	status := sdk.Status{
		Class:           sdk.StatusFailure,
		FailureClass:    failure.Class,
		SourceLatencyMS: latencyMillis(latency),
	}
	if failure.Terminal {
		state = sdk.ResultTerminal
	} else {
		status.RetryAfterSeconds = retryAfterSeconds(failure)
	}
	return sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           state,
		Claim:           claim,
		Generation:      sdk.Generation{ID: claim.GenerationID, ObservedAt: observedAt.UTC()},
		Statuses:        []sdk.Status{status},
	}
}

// budgetResult is the bounded terminal shape for exhausted budgets and bad
// configuration: an explicit terminal state with a named class.
func budgetResult(claim sdk.Claim, observedAt time.Time, class string) sdk.Result {
	return failureResult(claim, observedAt, Failure{Class: class, Terminal: true}, 0)
}

func retryAfterSeconds(failure Failure) int {
	if failure.RetryAfter > 0 {
		return int(math.Ceil(failure.RetryAfter.Seconds()))
	}
	if failure.Class == regfail.RegistryFailureRateLimited {
		return defaultRateLimitRetryAfterSeconds
	}
	return defaultRetryAfterSeconds
}

func latencyMillis(latency time.Duration) int {
	if latency <= 0 {
		return 0
	}
	return int(latency.Milliseconds())
}
