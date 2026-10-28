// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"
)

func blankGetenv(string) string { return "" }

// TestMapTargetMapsEveryProvider proves each registry provider maps to a
// usable target: the uncovered per-provider branches of the config decoder.
func TestMapTargetMapsEveryProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		target   targetJSON
		wantHost string
		wantRepo string
	}{
		{
			name: "dockerhub defaults the registry host",
			target: targetJSON{
				Provider:   "dockerhub",
				Repository: "library/busybox",
				SourceURI:  "urn:test:dockerhub",
			},
			wantHost: "docker.io",
			wantRepo: "library/busybox",
		},
		{
			name: "ghcr keeps an explicit registry",
			target: targetJSON{
				Provider:   "ghcr",
				Registry:   "ghcr.io",
				Repository: "owner/image",
				SourceURI:  "urn:test:ghcr",
			},
			wantHost: "ghcr.io",
			wantRepo: "owner/image",
		},
		{
			name: "jfrog resolves the repository identity",
			target: targetJSON{
				Provider:      "jfrog",
				BaseURL:       "https://jfrog.example.com",
				RepositoryKey: "docker-local",
				Repository:    "team/api",
				SourceURI:     "urn:test:jfrog",
			},
			wantHost: "https://jfrog.example.com/artifactory/api/docker/docker-local",
			wantRepo: "team/api",
		},
		{
			name: "ecr derives the registry host from account and region",
			target: targetJSON{
				Provider:   "ecr",
				RegistryID: "123456789012",
				Region:     "us-east-1",
				Repository: "team/api",
				SourceURI:  "urn:test:ecr",
			},
			wantHost: "123456789012.dkr.ecr.us-east-1.amazonaws.com",
			wantRepo: "team/api",
		},
		{
			name: "harbor resolves the project repository",
			target: targetJSON{
				Provider:   "harbor",
				BaseURL:    "https://harbor.example.com",
				Repository: "project/api",
				SourceURI:  "urn:test:harbor",
			},
			wantHost: "https://harbor.example.com",
			wantRepo: "project/api",
		},
		{
			name: "gar resolves the artifact registry host",
			target: targetJSON{
				Provider:     "google_artifact_registry",
				RegistryHost: "us-west1-docker.pkg.dev",
				Repository:   "project/repo/image",
				SourceURI:    "urn:test:gar",
			},
			wantHost: "https://us-west1-docker.pkg.dev",
			wantRepo: "project/repo/image",
		},
		{
			name: "acr resolves the container registry host",
			target: targetJSON{
				Provider:     "azure_container_registry",
				RegistryHost: "example.azurecr.io",
				Repository:   "samples/artifact",
				SourceURI:    "urn:test:acr",
			},
			wantHost: "https://example.azurecr.io",
			wantRepo: "samples/artifact",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := mapTarget(tc.target, blankGetenv)
			if err != nil {
				t.Fatalf("mapTarget() error = %v", err)
			}
			if got.Registry != tc.wantHost {
				t.Fatalf("Registry = %q, want %q", got.Registry, tc.wantHost)
			}
			if got.Repository != tc.wantRepo {
				t.Fatalf("Repository = %q, want %q", got.Repository, tc.wantRepo)
			}
			if _, err := scopeIDFor(got); err != nil {
				t.Fatalf("scopeIDFor() error = %v", err)
			}
		})
	}
}

// TestMapTargetRejectsBadTargets proves every mapper error branch fails
// closed without echoing credentials.
func TestMapTargetRejectsBadTargets(t *testing.T) {
	t.Parallel()

	longName := strings.Repeat("a", 129)
	cases := []struct {
		name   string
		target targetJSON
		want   string
	}{
		{
			name:   "unsupported provider",
			target: targetJSON{Provider: "example", SourceURI: "urn:test:x"},
			want:   "unsupported provider",
		},
		{
			name:   "blank repository",
			target: targetJSON{Provider: "dockerhub", SourceURI: "urn:test:x"},
			want:   "",
		},
		{
			name: "blank source URI",
			target: targetJSON{
				Provider:   "dockerhub",
				Repository: "library/busybox",
			},
			want: "source_uri is required",
		},
		{
			name: "bad username reference",
			target: targetJSON{
				Provider:    "dockerhub",
				Repository:  "library/busybox",
				SourceURI:   "urn:test:x",
				UsernameEnv: "not a name!",
			},
			want: "invalid environment variable name",
		},
		{
			name: "overlong password reference",
			target: targetJSON{
				Provider:    "dockerhub",
				Repository:  "library/busybox",
				SourceURI:   "urn:test:x",
				PasswordEnv: longName,
			},
			want: "invalid environment variable name",
		},
		{
			name: "bad bearer token reference",
			target: targetJSON{
				Provider:       "dockerhub",
				Repository:     "library/busybox",
				SourceURI:      "urn:test:x",
				BearerTokenEnv: "9bad",
			},
			want: "invalid environment variable name",
		},
		{
			name: "jfrog without base URL",
			target: targetJSON{
				Provider:      "jfrog",
				RepositoryKey: "docker-local",
				Repository:    "team/api",
				SourceURI:     "urn:test:x",
			},
			want: "",
		},
		{
			name: "harbor without project",
			target: targetJSON{
				Provider:   "harbor",
				BaseURL:    "https://harbor.example.com",
				Repository: "lonely",
				SourceURI:  "urn:test:x",
			},
			want: "",
		},
		{
			name: "gar with short repository",
			target: targetJSON{
				Provider:     "google_artifact_registry",
				RegistryHost: "us-west1-docker.pkg.dev",
				Repository:   "project/repo",
				SourceURI:    "urn:test:x",
			},
			want: "",
		},
		{
			name: "acr without repository",
			target: targetJSON{
				Provider:     "azure_container_registry",
				RegistryHost: "example.azurecr.io",
				SourceURI:    "urn:test:x",
			},
			want: "",
		},
		{
			name: "ecr without region",
			target: targetJSON{
				Provider:   "ecr",
				RegistryID: "123456789012",
				Repository: "team/api",
				SourceURI:  "urn:test:x",
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := mapTarget(tc.target, blankGetenv)
			if err == nil {
				t.Fatal("mapTarget() error = nil, want an error")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("mapTarget() error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

// TestResolveTargetRejectsEmptyTargets proves an empty target list is a
// config error, not a silent no-op scan.
func TestResolveTargetRejectsEmptyTargets(t *testing.T) {
	t.Parallel()

	_, _, err := resolveTarget(map[string]any{"targets": []any{}}, "scope", blankGetenv)
	if err == nil || !strings.Contains(err.Error(), "at least one target") {
		t.Fatalf("resolveTarget() error = %v, want the empty-targets rejection", err)
	}
}

// TestResolveTargetWrapsTargetErrors proves per-target failures name the
// index: a bad provider and an unmappable scope each fail closed.
func TestResolveTargetWrapsTargetErrors(t *testing.T) {
	t.Parallel()

	_, _, err := resolveTarget(map[string]any{
		"targets": []any{map[string]any{
			"provider":   "example",
			"source_uri": "urn:test:x",
		}},
	}, "scope", blankGetenv)
	if err == nil || !strings.Contains(err.Error(), "targets[0]") {
		t.Fatalf("resolveTarget() error = %v, want the targets[0] wrap", err)
	}

	// ECR maps without a repository check, so the scope identity step is
	// the one that rejects the blank repository.
	_, _, err = resolveTarget(map[string]any{
		"provider":    "ecr",
		"registry_id": "123456789012",
		"region":      "us-east-1",
		"source_uri":  "urn:test:x",
	}, "scope", blankGetenv)
	if err == nil || !strings.Contains(err.Error(), "targets[0]") {
		t.Fatalf("resolveTarget() error = %v, want the targets[0] scope wrap", err)
	}
}

// TestDecodeTargetsRejectsNilAndUnmarshalable proves the decoder head fails
// closed on a missing config and on values JSON cannot encode.
func TestDecodeTargetsRejectsNilAndUnmarshalable(t *testing.T) {
	t.Parallel()

	if _, _, err := decodeTargets(nil); err == nil {
		t.Fatal("decodeTargets(nil) error = nil, want an error")
	}
	_, _, err := decodeTargets(map[string]any{
		"targets": []any{},
		"poison":  func() {},
	})
	if err == nil || !strings.Contains(err.Error(), "malformed JSON") {
		t.Fatalf("decodeTargets() error = %v, want malformed JSON", err)
	}
	if _, err := decodeTopLevelLimits(map[string]any{"poison": func() {}}); err == nil {
		t.Fatal("decodeTopLevelLimits() error = nil, want an error")
	}
}

// TestDecodeReasonKeepsFailuresBounded proves every reason branch stays free
// of operator values: type errors name only the field, flexible vocabulary
// errors pass through verbatim, and anything else is malformed JSON.
func TestDecodeReasonKeepsFailuresBounded(t *testing.T) {
	t.Parallel()

	if _, _, err := decodeTargets(map[string]any{"targets": "nope"}); err == nil ||
		!strings.Contains(err.Error(), "field targets has the wrong type") {
		t.Fatalf("decodeTargets() error = %v, want the bounded type error", err)
	}
	if _, err := decodeTopLevelLimits(map[string]any{"max_records_per_claim": "lots"}); err == nil ||
		!strings.Contains(err.Error(), "field max_records_per_claim has the wrong type") {
		t.Fatalf("decodeTopLevelLimits() error = %v, want the bounded field error", err)
	}
	if got := firstNonBlank("", "  "); got != "" {
		t.Fatalf("firstNonBlank() = %q, want empty", got)
	}
	if got, err := fencingToken("  "); err != nil || got != 0 {
		t.Fatalf("fencingToken() = %d, %v, want zero without error", got, err)
	}
	if got, err := fencingToken("42"); err != nil || got != 42 {
		t.Fatalf("fencingToken() = %d, %v, want 42 without error", got, err)
	}
	if _, err := fencingToken("abc"); err == nil {
		t.Fatal("fencingToken() error = nil, want an error")
	}
}

// TestResourceUseUnmarshalJSONRejectsWrongTypes proves every bound rejects a
// mistyped value in either spelling without naming anything but the field.
func TestResourceUseUnmarshalJSONRejectsWrongTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"null stays zero", `null`, ""},
		{"snake wrong type", `{"max_records_per_claim":"lots"}`, "field max_records_per_claim has the wrong type"},
		{"camel wrong type", `{"maxRecordsPerClaim":"lots"}`, "field max_records_per_claim has the wrong type"},
		{"both spellings wrong camel", `{"max_records_per_claim":5,"maxRecordsPerClaim":"lots"}`, "field max_records_per_claim has the wrong type"},
		{"both spellings wrong snake", `{"max_records_per_claim":"lots","maxRecordsPerClaim":5}`, "field max_records_per_claim has the wrong type"},
		{"payload wrong type", `{"max_payload_bytes":[1]}`, "field max_payload_bytes has the wrong type"},
		{"result wrong type", `{"max_result_bytes":[1]}`, "field max_result_bytes has the wrong type"},
		{"timeout wrong type", `{"claim_timeout_seconds":[1]}`, "field claim_timeout_seconds has the wrong type"},
		{"non object", `[1]`, "cannot unmarshal array"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got ResourceUse
			err := got.UnmarshalJSON([]byte(tc.raw))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("UnmarshalJSON() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("UnmarshalJSON() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
