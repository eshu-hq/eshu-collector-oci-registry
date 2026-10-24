// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	collector "github.com/eshu-hq/eshu-collector-oci-registry"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// fakeRegistry serves the minimum OCI Distribution surface the collector
// exercises over plain HTTP, shaped like a JFrog Artifactory Docker API base.
func fakeRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	const (
		base         = "/artifactory/api/docker/demo"
		repository   = "team/api"
		reference    = "latest"
		digest       = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		configDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    configDigest,
			"size":      256,
		},
		"layers": []map[string]any{{
			"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
			"digest":    "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"size":      1024,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case base + "/v2/":
			w.WriteHeader(http.StatusOK)
		case base + "/v2/" + repository + "/tags/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"`+repository+`","tags":["`+reference+`"]}`)
		case base + "/v2/" + repository + "/manifests/" + reference:
			w.Header().Set("Docker-Content-Digest", digest)
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_, _ = w.Write(manifest)
		case base + "/v2/" + repository + "/referrers/" + digest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			_, _ = io.WriteString(w, `{"manifests":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func claimFor(serverURL string) sdk.Claim {
	host := strings.TrimPrefix(serverURL, "http://")
	host = strings.ToLower(host)
	return sdk.Claim{
		ComponentID: collector.ComponentID, InstanceID: "oci-qa", CollectorKind: collector.CollectorKind,
		SourceSystem: collector.SourceSystem,
		Scope: sdk.Scope{
			ID:   "oci-registry://" + host + "/artifactory/api/docker/demo/team/api",
			Kind: "container_registry_repository",
		},
		SourceRunID: "run-1", GenerationID: "generation-1",
		WorkItemID: "work-1", FencingToken: "7", Attempt: 1,
		Deadline: time.Now().Add(time.Hour).UTC(), ConfigHandle: "component-config:oci-registry",
	}
}

func requestFor(t *testing.T, baseURL string) string {
	t.Helper()
	raw, err := json.Marshal(collector.Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1, Claim: claimFor(baseURL), Contract: collector.Contract(),
		Config: map[string]any{
			"provider": "jfrog", "base_url": baseURL, "repository_key": "demo",
			"repository": "team/api", "tag_limit": 10, "source_uri": "https://jfrog.example/artifactory/api/docker/demo/v2/team/api",
			"username_env": "OCI_USER", "password_env": "OCI_PASSWORD",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func getenv(key string) string {
	return map[string]string{"OCI_USER": "bot", "OCI_PASSWORD": "pw-SECRET-123"}[key]
}

func TestRunClaimWritesOneValidResultToStdout(t *testing.T) {
	t.Parallel()

	server := fakeRegistry(t)
	for _, args := range [][]string{nil, {"--sdk-stdio"}} {
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), args, strings.NewReader(requestFor(t, server.URL)), &stdout, &stderr, getenv)
		if err != nil {
			t.Fatalf("run(%v) error = %v", args, err)
		}
		decoder := json.NewDecoder(&stdout)
		decoder.DisallowUnknownFields()
		var result sdk.Result
		if err := decoder.Decode(&result); err != nil {
			t.Fatalf("stdout is not one SDK result: %v", err)
		}
		if decoder.More() {
			t.Fatal("stdout carries more than one JSON value")
		}
		if result.State != sdk.ResultComplete || len(result.Facts) == 0 {
			t.Fatalf("State=%q facts=%d, want complete with facts", result.State, len(result.Facts))
		}
		if _, err := sdk.NewValidator(collector.Contract()).ValidateResult(result); err != nil {
			t.Fatalf("ValidateResult() error = %v", err)
		}
		for _, text := range []string{stdout.String(), stderr.String()} {
			if strings.Contains(text, "pw-SECRET-123") {
				t.Fatalf("output leaks a credential: %s", text)
			}
		}
		if lines := strings.Count(strings.TrimSpace(stderr.String()), "\n"); lines != 0 {
			t.Fatalf("stderr = %q, want exactly one log line", stderr.String())
		}
	}
}

func TestRunClaimReportsConfigFailureAsTerminalResultNotCrash(t *testing.T) {
	t.Parallel()

	// An unsupported provider can never succeed on retry: the binary reports
	// a bounded terminal result instead of crashing.
	raw := strings.Replace(requestFor(t, "http://127.0.0.1:1"), `"provider":"jfrog"`, `"provider":"example"`, 1)
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), nil, strings.NewReader(raw), &stdout, &stderr, func(string) string { return "" })
	if err != nil {
		t.Fatalf("run() error = %v; a config failure is a bounded terminal result", err)
	}
	var result sdk.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != sdk.ResultTerminal || result.Statuses[0].FailureClass != collector.FailureInvalidConfig {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunClaimRejectsBadInvocation(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args  []string
		stdin string
	}{
		"empty stdin":      {nil, ""},
		"garbage stdin":    {nil, "{nope"},
		"extra argument":   {[]string{"serve"}, "{}"},
		"unknown flag":     {[]string{"--nope"}, "{}"},
		"wrong protocol":   {nil, `{"protocol_version":"x"}`},
		"unknown seed arg": {[]string{"seed", "extra"}, ""},
		"seed no dest":     {[]string{"seed"}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if err := run(context.Background(), tc.args, strings.NewReader(tc.stdin), &stdout, &stderr, getenv); err == nil {
				t.Fatal("run() error = nil, want failure")
			}
			if stdout.Len() != 0 {
				t.Fatalf("a rejected invocation wrote %q to stdout", stdout.String())
			}
		})
	}
}
