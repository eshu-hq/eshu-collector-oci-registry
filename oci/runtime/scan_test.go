// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu-collector-oci-registry/internal/regfail"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/distribution"
)

func TestSourceNextDeclaresTagListTruncationInBand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		tags              []string
		tagLimit          int
		tagListIncomplete bool
		wantWarning       bool
		wantTags          []string
	}{
		{
			name:        "over limit",
			tags:        []string{"v2", "v1"},
			tagLimit:    1,
			wantWarning: true,
			wantTags:    []string{"v1"},
		},
		{
			name:              "short page below limit with continuation",
			tags:              []string{"v1"},
			tagLimit:          3,
			tagListIncomplete: true,
			wantWarning:       true,
			wantTags:          []string{"v1"},
		},
		{
			name:              "empty incomplete page",
			tagLimit:          3,
			tagListIncomplete: true,
			wantWarning:       true,
		},
		{
			name:        "exactly at limit",
			tags:        []string{"v1"},
			tagLimit:    1,
			wantWarning: false,
			wantTags:    []string{"v1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &stubRegistryClient{
				tags:              tt.tags,
				tagListIncomplete: tt.tagListIncomplete,
				manifest: distribution.ManifestResponse{
					Digest:    parityManifestDigest,
					MediaType: oci.MediaTypeOCIImageManifest,
					Body:      parityManifestBody(t),
					SizeBytes: 512,
				},
			}
			source := Source{
				Config: Config{
					CollectorInstanceID: "oci-runtime-truncation-test",
					Targets: []TargetConfig{{
						Provider:     oci.ProviderGHCR,
						Registry:     "registry.example.com",
						Repository:   "team/api",
						TagLimit:     tt.tagLimit,
						FencingToken: 11,
					}},
				},
				ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
					return client, nil
				}),
				Clock: func() time.Time {
					return time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
				},
			}

			collected, ok, err := source.Next(context.Background())
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if !ok {
				t.Fatal("Next() ok = false, want true")
			}

			var warnings []sdk.Fact
			var observedTags []string
			for _, fact := range collected.Facts {
				switch fact.Kind {
				case oci.FactKindWarning:
					if fact.Payload["warning_code"] == oci.WarningTagListTruncated {
						warnings = append(warnings, fact)
					}
				case oci.FactKindTagObservation:
					if tag, ok := fact.Payload["tag"].(string); ok {
						observedTags = append(observedTags, tag)
					}
				}
			}
			if got := len(warnings); got != boolCount(tt.wantWarning) {
				t.Fatalf("tag-list truncation warnings = %d, want %d", got, boolCount(tt.wantWarning))
			}
			if !slices.Equal(observedTags, tt.wantTags) {
				t.Fatalf("observed tags = %v, want %v", observedTags, tt.wantTags)
			}
			if !tt.wantWarning {
				return
			}
			warning := warnings[0]
			if got, want := warning.Payload["repository_id"], "oci-registry://registry.example.com/team/api"; got != want {
				t.Fatalf("warning repository_id = %v, want %q", got, want)
			}
			if got := warning.Payload["digest"]; got != "" {
				t.Fatalf("warning digest = %v, want blank repository-level warning", got)
			}
		})
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestSourceNextComputesDigestAndWarnsWhenManifestDigestHeaderIsMissing(t *testing.T) {
	t.Parallel()

	client := &stubRegistryClient{
		tags: []string{"latest"},
		manifest: distribution.ManifestResponse{
			MediaType: oci.MediaTypeOCIImageManifest,
			Body:      parityManifestBody(t),
		},
	}
	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-runtime-test",
			Targets: []TargetConfig{{
				Provider:   oci.ProviderGHCR,
				Registry:   "ghcr.io",
				Repository: "stargz-containers/busybox",
				TagLimit:   1,
			}},
		},
		ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
			return client, nil
		}),
		Clock: func() time.Time { return time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC) },
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	var warnings []string
	for _, fact := range collected.Facts {
		if fact.Kind != oci.FactKindWarning {
			continue
		}
		if code, ok := fact.Payload["warning_code"].(string); ok {
			warnings = append(warnings, code)
		}
	}
	if !slices.Contains(warnings, oci.WarningComputedManifestDigest) {
		t.Fatalf("warning codes = %v, want %q", warnings, oci.WarningComputedManifestDigest)
	}
}

func TestSourceNextPreservesImageConfigProvenanceLabels(t *testing.T) {
	t.Parallel()

	client := &configBlobRegistryClient{
		tags: []string{"prod"},
		manifest: distribution.ManifestResponse{
			Digest:    parityManifestDigest,
			MediaType: oci.MediaTypeOCIImageManifest,
			Body:      parityManifestBody(t),
			SizeBytes: 512,
		},
		blobs: map[string]distribution.BlobResponse{
			parityConfigDigest: {
				Body: configBlobBody(t, map[string]string{
					"org.opencontainers.image.source":   "https://github.com/acme/payments-api",
					"org.opencontainers.image.revision": "0123456789abcdef0123456789abcdef01234567",
					"com.example.private.token":         "secret",
				}),
				MediaType: "application/vnd.oci.image.config.v1+json",
				Digest:    parityConfigDigest,
				SizeBytes: 256,
			},
		},
	}
	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-runtime-test",
			Targets: []TargetConfig{{
				Provider:   oci.ProviderGHCR,
				Registry:   "ghcr.io",
				Repository: "acme/payments-api",
				References: []string{"prod"},
			}},
		},
		ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
			return client, nil
		}),
		Clock: func() time.Time { return time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC) },
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	manifest := requireFactKind(t, collected.Facts, oci.FactKindManifest)
	labels, ok := manifest.Payload["config_labels"].(map[string]string)
	if !ok {
		t.Fatalf("config_labels = %#v, want map[string]string", manifest.Payload["config_labels"])
	}
	if got := labels["org.opencontainers.image.source"]; got != "https://github.com/acme/payments-api" {
		t.Fatalf("source label = %q", got)
	}
	if got := labels["org.opencontainers.image.revision"]; got != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("revision label = %q", got)
	}
	if got := labels["com.example.private.token"]; got != oci.RedactedValue {
		t.Fatalf("private label = %q, want redacted", got)
	}
	if got, want := client.blobCalls, 1; got != want {
		t.Fatalf("blobCalls = %d, want %d", got, want)
	}
}

func TestSourceNextWarnsForUnavailableAndOversizedImageConfig(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		configSize    int64
		blobBody      []byte
		blobErr       error
		wantWarning   string
		wantBlobCalls int
	}{
		{
			name:          "unavailable",
			configSize:    256,
			blobErr:       regfail.RegistryHTTPFailure("oci", "", "get_blob", 404, nil),
			wantWarning:   "config_blob_unavailable",
			wantBlobCalls: 1,
		},
		{
			name:          "oversized descriptor",
			configSize:    1048577,
			wantWarning:   "config_blob_oversized",
			wantBlobCalls: 0,
		},
		{
			name:          "oversized response",
			configSize:    256,
			blobBody:      make([]byte, 1048577),
			wantWarning:   "config_blob_oversized",
			wantBlobCalls: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := &configBlobRegistryClient{
				tags: []string{"prod"},
				manifest: distribution.ManifestResponse{
					Digest:    parityManifestDigest,
					MediaType: oci.MediaTypeOCIImageManifest,
					Body:      manifestBodyWithConfigSize(t, tc.configSize),
					SizeBytes: 512,
				},
				blobs: map[string]distribution.BlobResponse{
					parityConfigDigest: {
						Body:      tc.blobBody,
						MediaType: "application/vnd.oci.image.config.v1+json",
						Digest:    parityConfigDigest,
						SizeBytes: int64(len(tc.blobBody)),
					},
				},
				blobErr: tc.blobErr,
			}
			source := Source{
				Config: Config{
					CollectorInstanceID: "oci-runtime-test",
					Targets: []TargetConfig{{
						Provider:   oci.ProviderGHCR,
						Registry:   "ghcr.io",
						Repository: "acme/payments-api",
						References: []string{"prod"},
					}},
				},
				ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
					return client, nil
				}),
				Clock: func() time.Time { return time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC) },
			}

			collected, ok, err := source.Next(context.Background())
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if !ok {
				t.Fatal("Next() ok = false, want true")
			}
			if got, want := client.blobCalls, tc.wantBlobCalls; got != want {
				t.Fatalf("blobCalls = %d, want %d", got, want)
			}
			warnings := warningCodes(collected.Facts)
			if !slices.Contains(warnings, tc.wantWarning) {
				t.Fatalf("warning codes = %#v, want %q", warnings, tc.wantWarning)
			}
			manifest := requireFactKind(t, collected.Facts, oci.FactKindManifest)
			if labels := manifest.Payload["config_labels"]; labels != nil {
				t.Fatalf("config_labels = %#v, want nil for %s", labels, tc.name)
			}
		})
	}
}

type configBlobRegistryClient struct {
	tags      []string
	manifest  distribution.ManifestResponse
	referrers distribution.ReferrersResponse
	blobs     map[string]distribution.BlobResponse
	blobErr   error
	blobCalls int
}

func (c *configBlobRegistryClient) Ping(context.Context) error { return nil }

func (c *configBlobRegistryClient) ListTags(context.Context, string, int) (distribution.TagListResponse, error) {
	return distribution.TagListResponse{Tags: append([]string(nil), c.tags...), Complete: true}, nil
}

func (c *configBlobRegistryClient) GetManifest(context.Context, string, string) (distribution.ManifestResponse, error) {
	return c.manifest, nil
}

func (c *configBlobRegistryClient) GetBlob(_ context.Context, _ string, digest string) (distribution.BlobResponse, error) {
	c.blobCalls++
	if c.blobErr != nil {
		return distribution.BlobResponse{}, c.blobErr
	}
	return c.blobs[digest], nil
}

func (c *configBlobRegistryClient) ListReferrers(context.Context, string, string) (distribution.ReferrersResponse, error) {
	return c.referrers, nil
}

func configBlobBody(t *testing.T, labels map[string]string) []byte {
	t.Helper()
	body := map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"config": map[string]any{
			"Labels": labels,
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal config blob: %v", err)
	}
	return encoded
}

func manifestBodyWithConfigSize(t *testing.T, configSize int64) []byte {
	t.Helper()
	body := map[string]any{
		"schemaVersion": 2,
		"mediaType":     oci.MediaTypeOCIImageManifest,
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    parityConfigDigest,
			"size":      configSize,
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return encoded
}

func requireFactKind(t *testing.T, facts []sdk.Fact, kind string) sdk.Fact {
	t.Helper()
	for _, fact := range facts {
		if fact.Kind == kind {
			return fact
		}
	}
	t.Fatalf("fact kind %q missing from %d facts", kind, len(facts))
	return sdk.Fact{}
}

func warningCodes(facts []sdk.Fact) []string {
	var out []string
	for _, fact := range facts {
		if fact.Kind != oci.FactKindWarning {
			continue
		}
		if code, ok := fact.Payload["warning_code"].(string); ok && code != "" {
			out = append(out, code)
		}
	}
	return out
}
