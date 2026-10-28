// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ecr

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewDistributionClientFetchesManifestWithBasicAuth proves the
// constructor resolves an ECR registry host and the returned client
// authenticates Distribution calls with the configured basic credentials.
func TestNewDistributionClientFetchesManifestWithBasicAuth(t *testing.T) {
	t.Parallel()

	const digest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	var sawAuth, sawPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawPath = r.URL.Path
		if sawAuth == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		w.Header().Set("Docker-Content-Digest", digest)
		_, _ = w.Write([]byte(`{"schemaVersion":2}`))
	}))
	defer server.Close()

	client, err := NewDistributionClient(server.URL, "AWS", "secret-password", server.Client())
	if err != nil {
		t.Fatalf("NewDistributionClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewDistributionClient() = nil")
	}
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v (sawAuth=%q)", err, sawAuth)
	}
	manifest, err := client.GetManifest(context.Background(), "team/api", digest)
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
	if manifest.Digest != digest {
		t.Fatalf("Digest = %q, want %q", manifest.Digest, digest)
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("AWS:secret-password")); sawAuth != want {
		t.Fatalf("Authorization = %q, want %q", sawAuth, want)
	}
	if !strings.Contains(sawPath, "/v2/team/api/manifests/") {
		t.Fatalf("manifest path = %q, want Distribution manifests path", sawPath)
	}
}

// TestNewDistributionClientRejectsInvalidHost covers the constructor's
// host error return.
func TestNewDistributionClientRejectsInvalidHost(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"", "http://123456789012.dkr.ecr.us-east-1.amazonaws.com"} {
		if _, err := NewDistributionClient(host, "AWS", "secret", nil); err == nil {
			t.Fatalf("NewDistributionClient(%q) error = nil", host)
		}
	}
}

// TestDistributionBaseURLTable is the table-driven seam for ECR host
// parsing: scheme promotion, normalization, and every rejection branch.
func TestDistributionBaseURLTable(t *testing.T) {
	t.Parallel()

	host := "123456789012.dkr.ecr.us-east-1.amazonaws.com"
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "bare host promotes to https", input: host, want: "https://" + host},
		{name: "trims spaces and slashes", input: "  " + host + "// ", want: "https://" + host},
		{name: "strips path query fragment", input: host + "/base?x=1#frag", want: "https://" + host + "/base"},
		{name: "empty", input: "", wantErr: true},
		{name: "unparseable", input: "https://" + host + "/\x7f", wantErr: true},
		{name: "http scheme", input: "http://" + host, wantErr: true},
		{name: "missing host", input: "https:///path", wantErr: true},
		{name: "credentials", input: "https://AWS:secret@" + host, wantErr: true},
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

// TestBasicAuthFromAuthorizationTokenTable covers the decode and shape
// rejection branches plus whitespace tolerance of the happy path.
func TestBasicAuthFromAuthorizationTokenTable(t *testing.T) {
	t.Parallel()

	encode := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	cases := []struct {
		name     string
		token    string
		wantUser string
		wantPass string
		wantErr  bool
	}{
		{name: "decodes credentials", token: encode("AWS:secret-password"), wantUser: "AWS", wantPass: "secret-password"},
		{name: "trims whitespace", token: "  " + encode("AWS:secret-password") + "\n", wantUser: "AWS", wantPass: "secret-password"},
		{name: "not base64", token: "%%%", wantErr: true},
		{name: "missing separator", token: encode("username-only"), wantErr: true},
		{name: "empty password", token: encode("AWS:"), wantErr: true},
		{name: "empty username", token: encode(":secret-password"), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			user, pass, err := BasicAuthFromAuthorizationToken(tc.token)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("BasicAuthFromAuthorizationToken() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("BasicAuthFromAuthorizationToken() error = %v", err)
			}
			if user != tc.wantUser || pass != tc.wantPass {
				t.Fatalf("credentials = %q/%q, want %q/%q", user, pass, tc.wantUser, tc.wantPass)
			}
		})
	}
}
