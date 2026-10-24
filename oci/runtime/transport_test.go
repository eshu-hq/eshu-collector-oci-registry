// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/eshu-hq/eshu-collector-oci-registry/internal/httpx"
	"github.com/eshu-hq/eshu-collector-oci-registry/internal/regfail"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/distribution"
)

// connectionReset builds the error net/http returns when the registry drops
// the TCP connection mid-request.
func connectionReset() error {
	return regfail.RegistryTransportFailure("oci", "", "ping", &url.Error{
		Op: "Get", URL: "https://registry.example.test/v2/",
		Err: &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)},
	})
}

// connectionRefused builds the error net/http returns when nothing listens on
// the registry address, the shape of a wrong host or port.
func connectionRefused() error {
	return regfail.RegistryTransportFailure("oci", "", "ping", &url.Error{
		Op: "Get", URL: "https://registry.internal.example/v2/",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)},
	})
}

// retryableStatus builds the error the distribution client returns for an
// HTTP status failure: a bounded RegistryFailure classified by status with
// the HTTP error carrying the status code.
func retryableStatus(t *testing.T, status int) error {
	t.Helper()
	return regfail.RegistryHTTPFailure("oci", "", "ping", status, httpx.HTTPError{
		Provider:   "oci",
		StatusCode: status,
		Message:    http.StatusText(status),
	})
}

type pingFailingClient struct {
	stubRegistryClient
	pingErr error
}

func (c *pingFailingClient) Ping(context.Context) error { return c.pingErr }

func transportTestSource(_ *testing.T, clients map[string]RegistryClient) *Source {
	return &Source{
		Config: Config{
			CollectorInstanceID: "oci-runtime-test",
			Targets: []TargetConfig{
				{Provider: oci.ProviderGHCR, Registry: "ghcr.io", Repository: "team/first", TagLimit: 1},
				{Provider: oci.ProviderGHCR, Registry: "ghcr.io", Repository: "team/second", TagLimit: 1},
			},
		},
		ClientFactory: ClientFactoryFunc(func(_ context.Context, target TargetConfig) (RegistryClient, error) {
			return clients[target.Repository], nil
		}),
	}
}

func healthyTransportClient(t *testing.T) *stubRegistryClient {
	t.Helper()
	return &stubRegistryClient{
		tags: []string{"latest"},
		manifest: distribution.ManifestResponse{
			Digest: parityManifestDigest, MediaType: oci.MediaTypeOCIImageManifest,
			Body: parityManifestBody(t), SizeBytes: 512,
		},
	}
}

func TestSourceNextSkipsTransientTargetWithinTheSameCall(t *testing.T) {
	t.Parallel()

	source := transportTestSource(t, map[string]RegistryClient{
		"team/first":  &pingFailingClient{pingErr: connectionReset()},
		"team/second": healthyTransportClient(t),
	})

	// A skipped target must not be reported as a drained batch: the next
	// target scans in the same call instead of after one more poll interval.
	result, ok, err := source.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next() = ok %v err %v, want the healthy second target scanned in the same call", ok, err)
	}
	if result.ScopeID != "oci-registry://ghcr.io/team/second" {
		t.Fatalf("ScopeID = %q, want the second target", result.ScopeID)
	}
}

func TestSourceNextEscalatesPersistentTransportFailureToFatal(t *testing.T) {
	t.Parallel()

	source := transportTestSource(t, map[string]RegistryClient{
		"team/first": &pingFailingClient{pingErr: connectionRefused()},
	})
	source.Config.Targets = source.Config.Targets[:1]

	limit := httpx.MaxConsecutiveTransportFailures
	for attempt := 1; attempt < limit; attempt++ {
		if _, ok, err := source.Next(context.Background()); err != nil || ok {
			t.Fatalf("attempt %d: Next() = ok %v err %v, want a skipped idle poll below the ceiling", attempt, ok, err)
		}
	}
	_, ok, err := source.Next(context.Background())
	if err == nil || ok {
		t.Fatalf("attempt %d: Next() = ok %v err %v, want a fatal error at the consecutive-failure ceiling", limit, ok, err)
	}
	if !strings.Contains(err.Error(), "consecutive") {
		t.Fatalf("Next() error = %q, want it to name the consecutive-failure ceiling", err)
	}
}

func TestSourceNextResetsTransportFailureCountAfterSuccess(t *testing.T) {
	t.Parallel()

	failing := &pingFailingClient{pingErr: connectionRefused()}
	healthy := healthyTransportClient(t)
	var client RegistryClient = failing
	source := transportTestSource(t, nil)
	source.Config.Targets = source.Config.Targets[:1]
	source.ClientFactory = ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
		return client, nil
	})

	limit := httpx.MaxConsecutiveTransportFailures
	for attempt := 1; attempt < limit; attempt++ {
		if _, _, err := source.Next(context.Background()); err != nil {
			t.Fatalf("attempt %d: Next() error = %v", attempt, err)
		}
	}
	client = healthy
	if _, ok, err := source.Next(context.Background()); err != nil || !ok {
		t.Fatalf("recovery Next() = ok %v err %v, want the target scanned", ok, err)
	}
	client = failing
	// A fresh outage after recovery starts a new consecutive run.
	for attempt := 1; attempt < limit; attempt++ {
		if _, _, err := source.Next(context.Background()); err != nil {
			t.Fatalf("post-recovery attempt %d: Next() error = %v, the count must have reset", attempt, err)
		}
	}
}

func TestSourceNextTransientLogCarriesAttemptAndCauseClass(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	source := transportTestSource(t, map[string]RegistryClient{
		"team/first": &pingFailingClient{pingErr: connectionRefused()},
	})
	source.Config.Targets = source.Config.Targets[:1]
	source.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	for range 2 {
		if _, _, err := source.Next(context.Background()); err != nil {
			t.Fatalf("Next() error = %v", err)
		}
	}
	out := logs.String()
	for _, want := range []string{`"consecutive_transport_failures":2`, `"cause_class":"refused"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("transient log missing %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "registry.internal.example") {
		t.Fatalf("transient log leaks the registry host:\n%s", out)
	}
}

func TestSourceNextKeepsCancellationAndNonTransportFailuresFatal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pingErr error
		cancel  bool
	}{
		{
			name: "certificate failure is misconfiguration not transport",
			pingErr: regfail.RegistryTransportFailure("oci", "", "ping", &url.Error{
				Op: "Get", Err: errors.New("x509: certificate signed by unknown authority"),
			}),
		},
		{
			name:    "empty success body is a content problem not transport",
			pingErr: fmt.Errorf("decode OCI tag list: %w", io.EOF),
		},
		{
			name:    "cancelled context",
			pingErr: connectionReset(),
			cancel:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := transportTestSource(t, map[string]RegistryClient{
				"team/first": &pingFailingClient{pingErr: tt.pingErr},
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			if _, _, err := source.Next(ctx); err == nil {
				t.Fatal("Next() error = nil, want the failure propagated")
			}
		})
	}
}

func TestSourceNextSkipsRetryableHTTPStatusOnPing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
	}{
		{name: "server error skips the target", status: http.StatusServiceUnavailable},
		{name: "rate limited skips the target", status: http.StatusTooManyRequests},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source := transportTestSource(t, map[string]RegistryClient{
				"team/first":  &pingFailingClient{pingErr: retryableStatus(t, tt.status)},
				"team/second": healthyTransportClient(t),
			})

			// The failed target is skipped for this cycle and the healthy
			// target scans in the same call.
			result, ok, err := source.Next(context.Background())
			if err != nil {
				t.Fatalf("Next() error = %v, want nil: an HTTP %d must not exit the collector", err, tt.status)
			}
			if !ok {
				t.Fatal("Next() ok = false, want the healthy target scanned after the status failure was skipped")
			}
			if result.ScopeID != "oci-registry://ghcr.io/team/second" {
				t.Fatalf("ScopeID = %q, want the second target", result.ScopeID)
			}
		})
	}
}

func TestSourceNextEscalatesPersistentRetryableStatusToFatal(t *testing.T) {
	t.Parallel()

	source := transportTestSource(t, map[string]RegistryClient{
		"team/first": &pingFailingClient{pingErr: retryableStatus(t, http.StatusServiceUnavailable)},
	})
	source.Config.Targets = source.Config.Targets[:1]

	limit := httpx.MaxConsecutiveTransportFailures
	for attempt := 1; attempt < limit; attempt++ {
		if _, ok, err := source.Next(context.Background()); err != nil || ok {
			t.Fatalf("attempt %d: Next() = ok %v err %v, want a skipped idle poll below the ceiling", attempt, ok, err)
		}
	}
	_, ok, err := source.Next(context.Background())
	if err == nil || ok {
		t.Fatalf("attempt %d: Next() = ok %v err %v, want a fatal error at the consecutive-failure ceiling", limit, ok, err)
	}
	if !strings.Contains(err.Error(), "consecutive") {
		t.Fatalf("Next() error = %q, want it to name the consecutive-failure ceiling", err)
	}
}

func TestSourceNextKeepsTerminalHTTPStatusFatal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		cancel bool
	}{
		{name: "bad request is terminal", status: http.StatusBadRequest},
		{name: "unauthorized is auth denial", status: http.StatusUnauthorized},
		{name: "not found is terminal", status: http.StatusNotFound},
		{name: "cancelled context stays fatal on retryable status", status: http.StatusServiceUnavailable, cancel: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := transportTestSource(t, map[string]RegistryClient{
				"team/first": &pingFailingClient{pingErr: retryableStatus(t, tt.status)},
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			if _, _, err := source.Next(ctx); err == nil {
				t.Fatal("Next() error = nil, want the failure propagated")
			}
		})
	}
}

func TestSourceNextRetryableStatusLogCarriesFailureClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		causeClass string
	}{
		{name: "server error", status: http.StatusServiceUnavailable, causeClass: regfail.RegistryFailureRetryable},
		{name: "rate limited", status: http.StatusTooManyRequests, causeClass: regfail.RegistryFailureRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			source := transportTestSource(t, map[string]RegistryClient{
				"team/first": &pingFailingClient{pingErr: retryableStatus(t, tt.status)},
			})
			source.Config.Targets = source.Config.Targets[:1]
			source.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

			if _, _, err := source.Next(context.Background()); err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			out := logs.String()
			if want := `"cause_class":"` + tt.causeClass + `"`; !strings.Contains(out, want) {
				t.Fatalf("transient log missing %s:\n%s", want, out)
			}
		})
	}
}
