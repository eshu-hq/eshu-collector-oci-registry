// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jfrog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewDistributionClientPingsArtifactoryEndpoint proves the constructor
// joins the base URL with the Artifactory Docker API path and the returned
// client authenticates Distribution calls with the configured credentials.
func TestNewDistributionClientPingsArtifactoryEndpoint(t *testing.T) {
	t.Parallel()

	var sawAuth, sawPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawPath = r.URL.Path
		if sawAuth == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewDistributionClient(Config{
		BaseURL:       server.URL,
		RepositoryKey: "docker-local",
		Username:      "reader",
		Password:      "secret",
		Client:        server.Client(),
	})
	if err != nil {
		t.Fatalf("NewDistributionClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewDistributionClient() = nil")
	}
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v (sawAuth=%q sawPath=%q)", err, sawAuth, sawPath)
	}
	if !strings.HasPrefix(sawAuth, "Basic ") {
		t.Fatalf("Authorization = %q, want Basic credentials", sawAuth)
	}
	if want := "/artifactory/api/docker/docker-local/v2/"; sawPath != want {
		t.Fatalf("Ping path = %q, want %q", sawPath, want)
	}
}

// TestNewDistributionClientRejectsInvalidBase covers the constructor's
// base-URL error return for a missing base URL and a missing key.
func TestNewDistributionClientRejectsInvalidBase(t *testing.T) {
	t.Parallel()

	if _, err := NewDistributionClient(Config{RepositoryKey: "docker-local"}); err == nil {
		t.Fatal("NewDistributionClient() error = nil for blank base URL")
	}
	if _, err := NewDistributionClient(Config{BaseURL: "https://example.jfrog.io"}); err == nil {
		t.Fatal("NewDistributionClient() error = nil for blank repository key")
	}
}

// TestDistributionBaseURLTable is the table-driven seam for Artifactory
// endpoint building: path joining plus every rejection branch.
func TestDistributionBaseURLTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		base    string
		key     string
		want    string
		wantErr bool
	}{
		{name: "joins nested base path", base: "https://example.jfrog.io/root/", key: "docker-local", want: "https://example.jfrog.io/root/artifactory/api/docker/docker-local"},
		{name: "trims key slashes", base: "https://example.jfrog.io", key: "/docker-local/", want: "https://example.jfrog.io/artifactory/api/docker/docker-local"},
		{name: "blank base", wantErr: true, key: "docker-local"},
		{name: "blank key", base: "https://example.jfrog.io", wantErr: true},
		{name: "unparseable base", base: "https://example.jfrog.io/\x7f", key: "docker-local", wantErr: true},
		{name: "missing scheme and host", base: "example.jfrog.io", key: "docker-local", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := DistributionBaseURL(tc.base, tc.key)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DistributionBaseURL(%q, %q) error = nil", tc.base, tc.key)
				}
				return
			}
			if err != nil {
				t.Fatalf("DistributionBaseURL(%q, %q) error = %v", tc.base, tc.key, err)
			}
			if got != tc.want {
				t.Fatalf("DistributionBaseURL(%q, %q) = %q, want %q", tc.base, tc.key, got, tc.want)
			}
		})
	}
}

// TestRepositoryIdentityRejectsInvalidBase covers the identity error return
// when the base URL is unusable.
func TestRepositoryIdentityRejectsInvalidBase(t *testing.T) {
	t.Parallel()

	if _, err := RepositoryIdentity("", "docker-local", "team/api"); err == nil {
		t.Fatal("RepositoryIdentity() error = nil for blank base URL")
	}
	if _, err := RepositoryIdentity("example.jfrog.io", "docker-local", "team/api"); err == nil {
		t.Fatal("RepositoryIdentity() error = nil for scheme-less base URL")
	}
}
