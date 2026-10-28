// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acr

import "testing"

// TestNewDistributionClientBuildsClientForValidHost proves the constructor
// resolves an ACR host into a usable Distribution client.
func TestNewDistributionClientBuildsClientForValidHost(t *testing.T) {
	t.Parallel()

	client, err := NewDistributionClient(Config{
		RegistryHost: "example.azurecr.io",
		Username:     "reader",
		Password:     "secret",
	})
	if err != nil {
		t.Fatalf("NewDistributionClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewDistributionClient() = nil")
	}
}

// TestNewDistributionClientRejectsInvalidHost covers the constructor's
// host error return.
func TestNewDistributionClientRejectsInvalidHost(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"", "http://example.azurecr.io", "registry.example.com"} {
		if _, err := NewDistributionClient(Config{RegistryHost: host}); err == nil {
			t.Fatalf("NewDistributionClient(%q) error = nil", host)
		}
	}
}

// TestDistributionBaseURLTable is the table-driven seam for ACR host
// parsing: scheme promotion, normalization, and every rejection branch.
func TestDistributionBaseURLTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "bare host promotes to https", input: "example.azurecr.io", want: "https://example.azurecr.io"},
		{name: "trims spaces and slashes", input: " https://example.azurecr.io/ ", want: "https://example.azurecr.io"},
		{name: "strips path query fragment", input: "example.azurecr.io/base?x=1#frag", want: "https://example.azurecr.io/base"},
		{name: "empty", input: "", wantErr: true},
		{name: "unparseable", input: "https://example.azurecr.io/\x7f", wantErr: true},
		{name: "http scheme", input: "http://example.azurecr.io", wantErr: true},
		{name: "missing host", input: "https:///path", wantErr: true},
		{name: "credentials", input: "https://user:secret@example.azurecr.io", wantErr: true},
		{name: "non-acr suffix", input: "registry.example.com", wantErr: true},
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

// TestRepositoryNameTable covers the empty and empty-segment rejection
// branches plus normalization of the happy path.
func TestRepositoryNameTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "normalizes case and slashes", input: " Samples/Artifact ", want: "samples/artifact"},
		{name: "empty", input: "", wantErr: true},
		{name: "slashes only", input: "///", wantErr: true},
		{name: "empty segments", input: "samples//artifact", wantErr: true},
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
// returns: an unusable registry host and an unusable repository.
func TestRepositoryIdentityRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	if _, err := RepositoryIdentity("", "samples/artifact"); err == nil {
		t.Fatal("RepositoryIdentity() error = nil for blank registry host")
	}
	if _, err := RepositoryIdentity("example.azurecr.io", ""); err == nil {
		t.Fatal("RepositoryIdentity() error = nil for blank repository")
	}
}
