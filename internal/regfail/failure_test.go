// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package regfail

import (
	"context"
	"errors"
	"testing"
)

// TestRegistryFailureErrorPrefersMessage proves Error surfaces the bounded
// operator message first, then the class, then a bare fallback, so status
// surfaces never render an empty string.
func TestRegistryFailureErrorPrefersMessage(t *testing.T) {
	t.Parallel()

	withMessage := RegistryFailure{Class: RegistryFailureNotFound, Message: "bounded message"}
	if got := withMessage.Error(); got != "bounded message" {
		t.Fatalf("Error() = %q, want the bounded message", got)
	}
	classOnly := RegistryFailure{Class: RegistryFailureRateLimited}
	if got := classOnly.Error(); got != "registry collector failed: "+RegistryFailureRateLimited {
		t.Fatalf("Error() = %q, want the class fallback", got)
	}
	if got := (RegistryFailure{}).Error(); got != "registry collector failed" {
		t.Fatalf("Error() = %q, want the bare fallback", got)
	}
}

// TestRegistryFailureAccessorsTrim proves the status accessors return trimmed
// values and Unwrap exposes the cause for errors.Is checks.
func TestRegistryFailureAccessorsTrim(t *testing.T) {
	t.Parallel()

	cause := errors.New("dial refused")
	failure := RegistryFailure{Class: "  " + RegistryFailureRetryable + " ", Details: " op=x ", Cause: cause}
	if got := failure.FailureClass(); got != RegistryFailureRetryable {
		t.Fatalf("FailureClass() = %q, want trimmed", got)
	}
	if got := failure.FailureDetails(); got != "op=x" {
		t.Fatalf("FailureDetails() = %q, want trimmed", got)
	}
	if !errors.Is(failure, cause) {
		t.Fatal("errors.Is(failure, cause) = false, want the unwrapped cause")
	}
}

// TestRegistryFailureClassForHTTPStatusCoversBands proves every status band
// maps to its stable class, so recovery tooling can switch on the class.
func TestRegistryFailureClassForHTTPStatusCoversBands(t *testing.T) {
	t.Parallel()

	cases := map[int]string{
		401: RegistryFailureAuthDenied,
		403: RegistryFailureAuthDenied,
		404: RegistryFailureNotFound,
		429: RegistryFailureRateLimited,
		408: RegistryFailureRetryable,
		500: RegistryFailureRetryable,
		503: RegistryFailureRetryable,
		200: RegistryFailureTerminal,
		400: RegistryFailureTerminal,
		302: RegistryFailureTerminal,
	}
	for status, want := range cases {
		if got := RegistryFailureClassForHTTPStatus(status); got != want {
			t.Errorf("RegistryFailureClassForHTTPStatus(%d) = %q, want %q", status, got, want)
		}
	}
}

// TestRegistryFailuresBoundDetails proves the constructors record bounded
// provider/operation context without the status code when there is none, and
// that cancellation maps to the canceled class.
func TestRegistryFailuresBoundDetails(t *testing.T) {
	t.Parallel()

	httpFailure := RegistryHTTPFailure("oci", "distribution", "tags", 404, errors.New("boom"))
	if httpFailure.Class != RegistryFailureNotFound {
		t.Fatalf("HTTP failure class = %q, want %q", httpFailure.Class, RegistryFailureNotFound)
	}
	if got := httpFailure.FailureDetails(); got == "" {
		t.Fatal("HTTP failure details are empty, want bounded context")
	}

	transient := RegistryTransportFailure("oci", "", "manifest", errors.New("reset"))
	if transient.Class != RegistryFailureRetryable {
		t.Fatalf("transport failure class = %q, want %q", transient.Class, RegistryFailureRetryable)
	}
	canceled := RegistryTransportFailure("oci", "", "manifest", context.Canceled)
	if canceled.Class != RegistryFailureCanceled {
		t.Fatalf("canceled failure class = %q, want %q", canceled.Class, RegistryFailureCanceled)
	}
}
