// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProviderFailureUnwrapRoundTrips(t *testing.T) {
	t.Parallel()

	cause := errors.New("dial tcp: connection refused")
	if got := (NewProviderFailure("oci", FailureRetryable, false, cause).Unwrap()); !errors.Is(got, cause) {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}

	if got := (NewProviderFailure("oci", FailureRetryable, false, nil).Unwrap()); got != nil {
		t.Errorf("Unwrap() of a causeless failure = %v, want nil", got)
	}
}

func TestProviderFailureRetryAfterWithoutGuidance(t *testing.T) {
	t.Parallel()

	failure := NewProviderFailure("oci", FailureRetryable, false, errors.New("boom"))
	if got := failure.RetryAfterDelay(); got != 0 {
		t.Errorf("RetryAfterDelay() = %s, want 0", got)
	}
}

func TestHTTPErrorTextStaysBounded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  HTTPError
		want string
	}{
		{"status form", HTTPError{Provider: "oci", StatusCode: 503}, "oci request failed with status 503"},
		{"blank provider defaults", HTTPError{StatusCode: 503}, "collector request failed with status 503"},
		{"message form", HTTPError{Provider: "oci", Message: "request failed"}, "oci request failed"},
		{"blank message defaults", HTTPError{Provider: "oci"}, "oci request failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}

	cause := errors.New("dial tcp")
	withCause := HTTPError{Provider: "oci", Message: "request failed", Cause: cause, RetryAfter: time.Second}
	if !errors.Is(withCause.Unwrap(), cause) {
		t.Error("Unwrap() must return the transport cause")
	}

	if withCause.RetryAfterDelay() != time.Second {
		t.Errorf("RetryAfterDelay() = %s, want 1s", withCause.RetryAfterDelay())
	}
}

func TestDoJSONRejectsMissingBaseURL(t *testing.T) {
	t.Parallel()

	err := DoJSON(t.Context(), JSONRequest{Provider: "oci", Operation: "list_tags"})

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want HTTPError", err)
	}

	if httpErr.Operation != "list_tags" || !strings.Contains(httpErr.Message, "build request") {
		t.Errorf("error = %+v, want the build-request failure with the operation kept", httpErr)
	}
}

func TestDoJSONRejectsUnencodableBody(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://example.invalid")
	if err != nil {
		t.Fatalf("parse base URL: %v", err)
	}

	err = DoJSON(t.Context(), JSONRequest{Provider: "oci", BaseURL: base, Body: func() {}})

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want HTTPError", err)
	}
}

type failingDoer struct{ err error }

func (d failingDoer) Do(_ *http.Request) (*http.Response, error) {
	return nil, d.err
}

func TestDoJSONWrapsTransportFailures(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://example.invalid")
	if err != nil {
		t.Fatalf("parse base URL: %v", err)
	}

	boom := errors.New("dial tcp: connection refused")
	err = DoJSON(t.Context(), JSONRequest{
		Provider:  "oci",
		BaseURL:   base,
		Endpoint:  "/v2/tags",
		Operation: "list_tags",
		Client:    failingDoer{err: boom},
	})

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want HTTPError", err)
	}

	if !errors.Is(err, boom) || httpErr.Operation != "list_tags" {
		t.Errorf("error = %+v, want the cause wrapped with the operation kept", httpErr)
	}
}

func TestDoJSONRejectsMalformedPayloads(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":`)
	}))
	t.Cleanup(server.Close)

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	var out map[string]any

	err = DoJSON(t.Context(), JSONRequest{Provider: "oci", BaseURL: base, Endpoint: "/v2/tags", Operation: "list_tags", Out: &out})

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want HTTPError", err)
	}

	if !strings.Contains(httpErr.Message, "decode response") || httpErr.Operation != "list_tags" {
		t.Errorf("error = %+v, want the decode failure with the operation kept", httpErr)
	}
}

func TestDoJSONSucceedsWithoutADecodeTarget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	if err := DoJSON(t.Context(), JSONRequest{Provider: "oci", BaseURL: base, Endpoint: "/v2/tags", Operation: "list_tags"}); err != nil {
		t.Fatalf("DoJSON() error = %v", err)
	}
}

func TestDoJSONPostsJSONBodies(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "want POST", http.StatusBadRequest)
			return
		}

		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "want JSON content type", http.StatusBadRequest)
			return
		}

		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name != "repo" {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":true}`)
	}))
	t.Cleanup(server.Close)

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	var out struct {
		Created bool `json:"created"`
	}

	err = DoJSON(t.Context(), JSONRequest{
		Provider:  "oci",
		Method:    http.MethodPost,
		BaseURL:   base,
		Endpoint:  "v2/repos",
		Operation: "create_repo",
		Body:      map[string]string{"name": "repo"},
		Out:       &out,
	})
	if err != nil {
		t.Fatalf("DoJSON() error = %v", err)
	}

	if !out.Created {
		t.Fatal("expected the created payload to decode")
	}
}

func TestStatusErrorHookControlsFailures(t *testing.T) {
	t.Parallel()

	hookErr := errors.New("custom status reading")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(server.Close)

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	// A hook error passes through untouched: the request token is not stamped.
	err = DoJSON(t.Context(), JSONRequest{
		Provider: "oci", BaseURL: base, Endpoint: "/v2/tags", Operation: "list_tags",
		StatusError: func(_ *http.Response) error { return hookErr },
	})
	if !errors.Is(err, hookErr) {
		t.Errorf("DoJSON() error = %v, want the hook error untouched", err)
	}

	// A hook-set token wins over the request token.
	hookToken := HTTPError{Provider: "oci", StatusCode: 418, Operation: "hook_token"}

	err = DoJSON(t.Context(), JSONRequest{
		Provider: "oci", BaseURL: base, Endpoint: "/v2/tags", Operation: "list_tags",
		StatusError: func(_ *http.Response) error { return hookToken },
	})

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want HTTPError", err)
	}

	if httpErr.Operation != "hook_token" {
		t.Errorf("operation = %q, want the hook token to win", httpErr.Operation)
	}
}

func TestWithRequestOperationStamping(t *testing.T) {
	t.Parallel()

	if err := withRequestOperation(nil, "list_tags"); err != nil {
		t.Errorf("nil error must stay nil, got %v", err)
	}

	sentinel := errors.New("boom")
	if err := withRequestOperation(sentinel, ""); !errors.Is(err, sentinel) {
		t.Errorf("empty operation must leave the error alone, got %v", err)
	}

	if err := withRequestOperation(sentinel, "list_tags"); !errors.Is(err, sentinel) {
		t.Errorf("a non-HTTP error must pass through, got %v", err)
	}

	stamped := withRequestOperation(HTTPError{Provider: "oci", StatusCode: 500}, "list_tags")

	var httpErr HTTPError
	if !errors.As(stamped, &httpErr) || httpErr.Operation != "list_tags" {
		t.Errorf("stamped error = %+v, want the request token", stamped)
	}
}

func TestDefaultClientHonorsTimeouts(t *testing.T) {
	t.Parallel()

	if got := DefaultClient(0); got.Timeout != DefaultTimeout {
		t.Errorf("zero timeout = %s, want %s", got.Timeout, DefaultTimeout)
	}

	if got := DefaultClient(5 * time.Second); got.Timeout != 5*time.Second {
		t.Errorf("timeout = %s, want 5s", got.Timeout)
	}
}

func TestParseRetryAfterHeaderUsesNow(t *testing.T) {
	t.Parallel()

	if got := ParseRetryAfterHeader("2"); got != 2*time.Second {
		t.Errorf("ParseRetryAfterHeader(\"2\") = %s, want 2s", got)
	}

	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if got := ParseRetryAfterHeader(future); got < 80*time.Second || got > 100*time.Second {
		t.Errorf("ParseRetryAfterHeader(%q) = %s, want ~90s", future, got)
	}

	if got := ParseRetryAfterHeader("not-a-date"); got != 0 {
		t.Errorf("ParseRetryAfterHeader() = %s, want 0", got)
	}
}

func TestClassifyProviderFailurePreservesHTTPOperation(t *testing.T) {
	t.Parallel()

	httpErr := HTTPError{Provider: "oci", StatusCode: 500, Operation: "get_manifest"}
	failure := ClassifyProviderFailure("oci", httpErr, StatusPolicy{}, FailureRetryable)

	if failure.FailureOperation() != "get_manifest" {
		t.Errorf("operation = %q, want the HTTP token kept", failure.FailureOperation())
	}

	// A status error that names no operation leaves the failure token blank.
	bare := HTTPError{Provider: "oci", StatusCode: 500}
	if got := ClassifyProviderFailure("oci", bare, StatusPolicy{}, FailureRetryable); got.FailureOperation() != "" {
		t.Errorf("operation = %q, want blank", got.FailureOperation())
	}

	// A terminal fallback is terminal; a blank fallback defaults to retryable.
	if got := ClassifyProviderFailure("oci", errors.New("boom"), StatusPolicy{}, FailureTerminal); !got.TerminalFailure() {
		t.Error("a terminal fallback must be terminal")
	}

	if got := ClassifyProviderFailure("oci", errors.New("boom"), StatusPolicy{}, ""); got.TerminalFailure() || got.FailureClass() != "retryable" {
		t.Errorf("a blank fallback must default to retryable, got %s terminal=%v", got.FailureClass(), got.TerminalFailure())
	}

	// Context deadlines stay retryable and keep no operation token.
	deadline := fmt.Errorf("do: %w", context.DeadlineExceeded)
	if got := ClassifyProviderFailure("oci", deadline, StatusPolicy{}, FailureRetryable); got.TerminalFailure() {
		t.Error("a deadline must stay retryable")
	}
}

func TestProviderFailureDefaultsBlankProvider(t *testing.T) {
	t.Parallel()

	failure := NewProviderFailure("  ", FailureTerminal, true, nil)
	if got := failure.Error(); got != "collector provider failure: terminal" {
		t.Errorf("Error() = %q, want the collector default", got)
	}
}

func TestParseRetryAfterDefaultsZeroNow(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if got := ParseRetryAfter(future, time.Time{}); got < 80*time.Second || got > 100*time.Second {
		t.Errorf("ParseRetryAfter(%q, zero) = %s, want ~90s", future, got)
	}
}

func TestDoJSONRejectsInvalidMethod(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://example.invalid")
	if err != nil {
		t.Fatalf("parse base URL: %v", err)
	}

	err = DoJSON(t.Context(), JSONRequest{Provider: "oci", BaseURL: base, Method: "a\x7fb", Operation: "list_tags"})

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want HTTPError", err)
	}
}

func TestDoJSONRejectsExactCapBody(t *testing.T) {
	t.Parallel()

	pad := strings.Repeat("x", int(MaxResponseBytes)+1-len(`{"pad":""`))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"pad":"`+pad+`"}`)
	}))
	t.Cleanup(server.Close)

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	var out map[string]any

	err = DoJSON(t.Context(), JSONRequest{Provider: "oci", BaseURL: base, Endpoint: "/v2/tags", Operation: "list_tags", Out: &out})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("error = %v, want ErrResponseTooLarge", err)
	}
}
