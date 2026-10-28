// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package harbor

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewDistributionClientPingsTLSRegistryWithBasicAuth proves the
// constructor resolves an HTTPS registry URL and the returned client
// authenticates Distribution calls with the configured basic credentials.
func TestNewDistributionClientPingsTLSRegistryWithBasicAuth(t *testing.T) {
	t.Parallel()

	var sawAuth string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v2/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if sawAuth == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewDistributionClient(Config{
		BaseURL:  server.URL,
		Username: "robot$reader",
		Password: "secret",
		Client:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewDistributionClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewDistributionClient() = nil")
	}
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("robot$reader:secret"))
	if sawAuth != want {
		t.Fatalf("Authorization = %q, want %q", sawAuth, want)
	}
}

// TestNewDistributionClientPingsTLSRegistryWithBearerToken proves a
// configured bearer token wins over basic credentials on registry calls.
func TestNewDistributionClientPingsTLSRegistryWithBearerToken(t *testing.T) {
	t.Parallel()

	var gotAuth string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewDistributionClient(Config{
		BaseURL:     server.URL,
		Username:    "robot",
		Password:    "secret",
		BearerToken: "harbor-token",
		Client:      server.Client(),
	})
	if err != nil {
		t.Fatalf("NewDistributionClient() error = %v", err)
	}
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if want := "Bearer harbor-token"; gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
}

// TestNewDistributionClientRejectsInvalidBaseURL covers the constructor's
// base-URL error return.
func TestNewDistributionClientRejectsInvalidBaseURL(t *testing.T) {
	t.Parallel()

	for _, baseURL := range []string{"", "http://harbor.example.com", "https://robot:secret@harbor.example.com"} {
		if _, err := NewDistributionClient(Config{BaseURL: baseURL}); err == nil {
			t.Fatalf("NewDistributionClient(%q) error = nil", baseURL)
		}
	}
}

// TestDistributionBaseURLTable is the table-driven seam for Harbor registry
// URL parsing: scheme promotion, normalization, and every rejection branch.
func TestDistributionBaseURLTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "bare host promotes to https", input: "harbor.example.com", want: "https://harbor.example.com"},
		{name: "trims spaces and slashes", input: " https://harbor.example.com/ ", want: "https://harbor.example.com"},
		{name: "strips query and fragment", input: "https://harbor.example.com/base?x=1#frag", want: "https://harbor.example.com/base"},
		{name: "empty", input: "", wantErr: true},
		{name: "blank slashes", input: " /// ", wantErr: true},
		{name: "unparseable", input: "https://harbor.example.com/\x7f", wantErr: true},
		{name: "http scheme", input: "http://harbor.example.com", wantErr: true},
		{name: "missing host", input: "https:///path", wantErr: true},
		{name: "credentials", input: "https://robot:secret@harbor.example.com", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := DistributionBaseURL(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DistributionBaseURL(%q) error = nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("DistributionBaseURL(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("DistributionBaseURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestRepositoryNameTable covers the empty and shape rejection branches plus
// normalization of the happy path.
func TestRepositoryNameTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "normalizes case and project path", input: " Project/API ", want: "project/api"},
		{name: "empty", input: "", wantErr: true},
		{name: "slashes only", input: "///", wantErr: true},
		{name: "missing project", input: "lonely", wantErr: true},
		{name: "empty segments", input: "project//api", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RepositoryName(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("RepositoryName(%q) error = nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("RepositoryName(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("RepositoryName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestRepositoryIdentityRejectsInvalidInputs covers both identity error
// returns: an unusable base URL and an unusable repository.
func TestRepositoryIdentityRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	if _, err := RepositoryIdentity("", "project/api"); err == nil {
		t.Fatal("RepositoryIdentity() error = nil for blank base URL")
	}
	if _, err := RepositoryIdentity("https://harbor.example.com", "lonely"); err == nil {
		t.Fatal("RepositoryIdentity() error = nil for project-less repository")
	}
}
