// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gar

import "testing"

// TestNewDistributionClientBuildsClientForValidHost proves the constructor
// resolves a GAR host into a usable Distribution client.
func TestNewDistributionClientBuildsClientForValidHost(t *testing.T) {
	t.Parallel()

	client, err := NewDistributionClient(Config{
		RegistryHost: "us-west1-docker.pkg.dev",
		Username:     "_json_key",
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

	for _, host := range []string{"", "http://us-west1-docker.pkg.dev", "registry.example.com"} {
		if _, err := NewDistributionClient(Config{RegistryHost: host}); err == nil {
			t.Fatalf("NewDistributionClient(%q) error = nil", host)
		}
	}
}

// TestDistributionBaseURLTable is the table-driven seam for GAR host
// parsing: scheme promotion, normalization, and every rejection branch.
func TestDistributionBaseURLTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "bare host promotes to https", input: "us-west1-docker.pkg.dev", want: "https://us-west1-docker.pkg.dev"},
		{name: "trims spaces and slashes", input: " https://us-west1-docker.pkg.dev/ ", want: "https://us-west1-docker.pkg.dev"},
		{name: "strips path query fragment", input: "us-west1-docker.pkg.dev/base?x=1#frag", want: "https://us-west1-docker.pkg.dev/base"},
		{name: "empty", input: "", wantErr: true},
		{name: "unparseable", input: "https://us-west1-docker.pkg.dev/\x7f", wantErr: true},
		{name: "http scheme", input: "http://us-west1-docker.pkg.dev", wantErr: true},
		{name: "missing host", input: "https:///path", wantErr: true},
		{name: "credentials", input: "https://_json_key:secret@us-west1-docker.pkg.dev", wantErr: true},
		{name: "non-gar suffix", input: "registry.example.com", wantErr: true},
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
		{name: "normalizes case", input: "Example-Project/Team-Api/Service", want: "example-project/team-api/service"},
		{name: "empty", input: "", wantErr: true},
		{name: "slashes only", input: "///", wantErr: true},
		{name: "missing image path", input: "project/repo", wantErr: true},
		{name: "empty segments", input: "project//repo/image", wantErr: true},
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

	if _, err := RepositoryIdentity("", "project/repo/image"); err == nil {
		t.Fatal("RepositoryIdentity() error = nil for blank registry host")
	}
	if _, err := RepositoryIdentity("us-west1-docker.pkg.dev", "project/repo"); err == nil {
		t.Fatal("RepositoryIdentity() error = nil for short repository")
	}
}
