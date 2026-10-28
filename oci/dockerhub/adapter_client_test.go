// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerhub

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// stubTokenTransport fakes both the Docker Hub token endpoint
// (auth.docker.io) and the registry endpoint (registry-1.docker.io) so
// NewDistributionClient runs its full production path without network
// access. The same *http.Client carries both calls, matching production
// wiring where one client serves the token fetch and Distribution calls.
type stubTokenTransport struct {
	token       string
	rawBody     string
	tokenStatus int
	tokenErr    error

	tokenCalls int
	sawService string
	sawScope   string
	sawUser    string
	sawPass    string
	sawAuth    string
}

func (s *stubTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if s.tokenErr != nil {
		return nil, s.tokenErr
	}
	if req.URL.Host == "auth.docker.io" {
		s.tokenCalls++
		user, pass, _ := req.BasicAuth()
		s.sawUser, s.sawPass = user, pass
		s.sawService = req.URL.Query().Get("service")
		s.sawScope = req.URL.Query().Get("scope")
		status := s.tokenStatus
		if status == 0 {
			status = http.StatusOK
		}
		body := s.rawBody
		if body == "" {
			if status == http.StatusOK {
				body = `{"token":` + `"` + s.token + `"}`
			} else {
				body = `{"message":"denied"}`
			}
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}
	s.sawAuth = req.Header.Get("Authorization")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// TestNewDistributionClientFetchesTokenAndPingsWithBearerAuth proves the
// constructor mints a pull token for the normalized repository and the
// returned client authenticates registry calls with that bearer token.
func TestNewDistributionClientFetchesTokenAndPingsWithBearerAuth(t *testing.T) {
	t.Parallel()

	stub := &stubTokenTransport{token: "test-bearer-token"}
	client, err := NewDistributionClient(context.Background(), Config{
		Repository: "busybox",
		Username:   "user",
		Password:   "pass",
		Client:     &http.Client{Transport: stub},
	})
	if err != nil {
		t.Fatalf("NewDistributionClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewDistributionClient() = nil")
	}
	if stub.tokenCalls != 1 {
		t.Fatalf("tokenCalls = %d, want 1", stub.tokenCalls)
	}
	if stub.sawService != tokenService {
		t.Fatalf("service = %q, want %q", stub.sawService, tokenService)
	}
	if want := "repository:library/busybox:pull"; stub.sawScope != want {
		t.Fatalf("scope = %q, want %q", stub.sawScope, want)
	}
	if stub.sawUser != "user" || stub.sawPass != "pass" {
		t.Fatalf("token BasicAuth = %q/%q, want user/pass", stub.sawUser, stub.sawPass)
	}
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if want := "Bearer test-bearer-token"; stub.sawAuth != want {
		t.Fatalf("registry Authorization = %q, want %q", stub.sawAuth, want)
	}
}

// TestNewDistributionClientRejectsInvalidRepository is the table-driven seam
// for the constructor's repository normalization failures: no token request
// leaves the process.
func TestNewDistributionClientRejectsInvalidRepository(t *testing.T) {
	t.Parallel()

	for _, repository := range []string{"", "   ", "/", "team//api"} {
		stub := &stubTokenTransport{token: "unused"}
		if _, err := NewDistributionClient(context.Background(), Config{
			Repository: repository,
			Client:     &http.Client{Transport: stub},
		}); err == nil {
			t.Fatalf("NewDistributionClient(%q) error = nil", repository)
		}
		if stub.tokenCalls != 0 {
			t.Fatalf("NewDistributionClient(%q) made %d token calls, want 0", repository, stub.tokenCalls)
		}
	}
}

// TestNewDistributionClientPropagatesTokenFailures proves token-service
// denials, empty token payloads, and transport errors surface as
// constructor errors instead of anonymous clients.
func TestNewDistributionClientPropagatesTokenFailures(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("dial denied for auth.docker.io")
	cases := []struct {
		name string
		stub *stubTokenTransport
	}{
		{name: "denied", stub: &stubTokenTransport{tokenStatus: http.StatusForbidden}},
		{name: "empty token", stub: &stubTokenTransport{rawBody: `{}`}},
		{name: "transport error", stub: &stubTokenTransport{tokenErr: transportErr}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewDistributionClient(context.Background(), Config{
				Repository: "team/api",
				Username:   "user",
				Password:   "pass",
				Client:     &http.Client{Transport: tc.stub},
			}); err == nil {
				t.Fatal("NewDistributionClient() error = nil, want token failure")
			}
		})
	}
}

// TestRepositoryIdentityRejectsInvalidRepository covers the identity error
// passthrough when normalization fails.
func TestRepositoryIdentityRejectsInvalidRepository(t *testing.T) {
	t.Parallel()

	for _, repository := range []string{"", "team//api"} {
		if _, err := RepositoryIdentity(repository); err == nil {
			t.Fatalf("RepositoryIdentity(%q) error = nil", repository)
		}
	}
}
