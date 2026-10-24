// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/distribution"
)

const (
	parityManifestDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	parityConfigDigest   = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	parityLayerDigest    = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	parityReferrerDigest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
)

var parityObservedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// stubRegistryClient serves canned Distribution responses without a socket.
type stubRegistryClient struct {
	tags              []string
	tagListIncomplete bool
	manifest          distribution.ManifestResponse
	manifestErr       error
	referrers         distribution.ReferrersResponse
	referrersErr      error
}

func (s *stubRegistryClient) Ping(context.Context) error { return nil }

func (s *stubRegistryClient) ListTags(_ context.Context, _ string, _ int) (distribution.TagListResponse, error) {
	return distribution.TagListResponse{
		Tags:     append([]string(nil), s.tags...),
		Complete: !s.tagListIncomplete,
	}, nil
}

func (s *stubRegistryClient) GetManifest(_ context.Context, _, _ string) (distribution.ManifestResponse, error) {
	if s.manifestErr != nil {
		return distribution.ManifestResponse{}, s.manifestErr
	}
	return s.manifest, nil
}

func (s *stubRegistryClient) GetBlob(context.Context, string, string) (distribution.BlobResponse, error) {
	return distribution.BlobResponse{}, nil
}

func (s *stubRegistryClient) ListReferrers(_ context.Context, _, _ string) (distribution.ReferrersResponse, error) {
	if s.referrersErr != nil {
		return distribution.ReferrersResponse{}, s.referrersErr
	}
	return s.referrers, nil
}

func parityManifestBody(t *testing.T) []byte {
	t.Helper()
	body := map[string]any{
		"schemaVersion": 2,
		"mediaType":     oci.MediaTypeOCIImageManifest,
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    parityConfigDigest,
			"size":      256,
		},
		"layers": []map[string]any{{
			"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
			"digest":    parityLayerDigest,
			"size":      1024,
		}},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal parity manifest: %v", err)
	}
	return encoded
}

// parityReferrerFailClient reports an unsupported Referrers API while serving
// every other call from the stub.
type parityReferrerFailClient struct {
	stubRegistryClient
	referrersErr error
}

func (c *parityReferrerFailClient) ListReferrers(context.Context, string, string) (distribution.ReferrersResponse, error) {
	return distribution.ReferrersResponse{}, c.referrersErr
}

// parityLiveSource rebuilds the three-target scan the parity pack pins: a
// full Docker Hub scan, a GHCR scan with an unsupported Referrers API, and a
// Harbor multi-arch index scan, all on the same fixed clock so generation IDs
// and stable keys reproduce deterministically.
func parityLiveSource(t *testing.T) *Source {
	t.Helper()
	manifest := func() distribution.ManifestResponse {
		return distribution.ManifestResponse{
			Digest:    parityManifestDigest,
			MediaType: oci.MediaTypeOCIImageManifest,
			Body:      parityManifestBody(t),
			SizeBytes: 512,
		}
	}
	fullClient := &stubRegistryClient{
		tags:     []string{"latest"},
		manifest: manifest(),
		referrers: distribution.ReferrersResponse{Referrers: []oci.Descriptor{{
			Digest:       parityReferrerDigest,
			MediaType:    oci.MediaTypeOCIImageManifest,
			SizeBytes:    128,
			ArtifactType: "application/vnd.in-toto+json",
		}}},
	}
	partialClient := &parityReferrerFailClient{
		stubRegistryClient: stubRegistryClient{
			tags:     []string{"v2", "v1"},
			manifest: manifest(),
		},
		// Keep byte-identical with the in-tree pack: the error text feeds
		// the warning message and therefore its stable key.
		referrersErr: errors.New("referrers API not supported by this registry"),
	}
	indexBody, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     oci.MediaTypeOCIImageIndex,
		"manifests": []map[string]any{
			{
				"mediaType": oci.MediaTypeOCIImageManifest,
				"digest":    parityManifestDigest,
				"size":      512,
			},
			{
				"mediaType": oci.MediaTypeOCIImageManifest,
				"digest":    parityConfigDigest,
				"size":      256,
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal index body: %v", err)
	}
	indexClient := &stubRegistryClient{
		tags: []string{"multiarch"},
		manifest: distribution.ManifestResponse{
			Digest:    parityReferrerDigest,
			MediaType: oci.MediaTypeOCIImageIndex,
			Body:      indexBody,
			SizeBytes: 1024,
		},
	}
	return &Source{
		Config: Config{
			CollectorInstanceID: "oci-parity-8861",
			Targets: []TargetConfig{
				{
					Provider:     oci.ProviderDockerHub,
					Registry:     "registry-1.docker.io",
					Repository:   "library/busybox",
					TagLimit:     10,
					Visibility:   oci.VisibilityPublic,
					AuthMode:     oci.AuthModeAnonymous,
					SourceURI:    "https://registry-1.docker.io/v2/library/busybox",
					FencingToken: 7,
				},
				{
					Provider:     oci.ProviderGHCR,
					Registry:     "ghcr.io",
					Repository:   "eshu-hq/parity-demo",
					TagLimit:     1,
					Visibility:   oci.VisibilityPublic,
					AuthMode:     oci.AuthModeAnonymous,
					SourceURI:    "https://ghcr.io/v2/eshu-hq/parity-demo",
					FencingToken: 9,
				},
				{
					Provider:     oci.ProviderHarbor,
					Registry:     "harbor.example.test",
					Repository:   "team/multiarch",
					TagLimit:     10,
					Visibility:   oci.VisibilityPrivate,
					AuthMode:     oci.AuthModeCredentialed,
					SourceURI:    "https://harbor.example.test/v2/team/multiarch",
					FencingToken: 11,
				},
			},
		},
		ClientFactory: ClientFactoryFunc(func(_ context.Context, target TargetConfig) (RegistryClient, error) {
			switch target.Repository {
			case "eshu-hq/parity-demo":
				return partialClient, nil
			case "team/multiarch":
				return indexClient, nil
			}
			return fullClient, nil
		}),
		Clock: func() time.Time { return parityObservedAt },
	}
}

// drainResults collects the scan outputs of one full pass.
func drainResults(t *testing.T, source *Source) []ScanResult {
	t.Helper()
	var results []ScanResult
	for {
		result, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			return results
		}
		results = append(results, result)
	}
}

func stableKeysOf(results []ScanResult) []string {
	var keys []string
	seen := map[string]bool{}
	for _, result := range results {
		for _, fact := range result.Facts {
			if !seen[fact.StableKey] {
				seen[fact.StableKey] = true
				keys = append(keys, fact.StableKey)
			}
		}
	}
	slices.Sort(keys)
	return keys
}

// TestScanParityCoversAllSevenKinds pins the emission half of the #8861
// parity pack: three fixed scans cover all 7 kinds at 1.0.0 with reported
// confidence, and every fact carries its scope and generation boundary.
func TestScanParityCoversAllSevenKinds(t *testing.T) {
	t.Parallel()

	results := drainResults(t, parityLiveSource(t))
	if len(results) != 3 {
		t.Fatalf("scopes = %d, want 3", len(results))
	}
	union := map[string]bool{}
	for _, result := range results {
		if result.ScopeKind != ScopeKindContainerRegistryRepository {
			t.Fatalf("ScopeKind = %q, want %q", result.ScopeKind, ScopeKindContainerRegistryRepository)
		}
		if len(result.Generation.ID) < 13 || result.Generation.ID[:13] != "oci-registry:" {
			t.Fatalf("GenerationID = %q, want the oci-registry: prefix", result.Generation.ID)
		}
		if result.Generation.FreshnessHint == "" {
			t.Fatalf("scope %q has an empty freshness hint", result.ScopeID)
		}
		if len(result.Facts) == 0 {
			t.Fatalf("scope %q has no facts", result.ScopeID)
		}
		for _, fact := range result.Facts {
			union[fact.Kind] = true
			if fact.SchemaVersion != oci.SchemaVersion {
				t.Fatalf("fact %q SchemaVersion = %q, want %q", fact.Kind, fact.SchemaVersion, oci.SchemaVersion)
			}
			if fact.SourceConfidence != "reported" {
				t.Fatalf("fact %q SourceConfidence = %q, want reported", fact.Kind, fact.SourceConfidence)
			}
			if fact.StableKey == "" {
				t.Fatalf("fact %q has a blank stable key", fact.Kind)
			}
			if fact.SourceRef.ScopeID != result.ScopeID {
				t.Fatalf("fact %q SourceRef.ScopeID = %q, want %q", fact.Kind, fact.SourceRef.ScopeID, result.ScopeID)
			}
			if fact.SourceRef.GenerationID != result.Generation.ID {
				t.Fatalf("fact %q SourceRef.GenerationID = %q, want %q", fact.Kind, fact.SourceRef.GenerationID, result.Generation.ID)
			}
		}
	}
	for _, kind := range oci.FactKinds() {
		if !union[kind] {
			t.Fatalf("parity pack covers no %q fact", kind)
		}
	}
}

// TestScanParityDuplicateDeliveryIsStable pins duplicate safety: two identical
// scans emit identical generation IDs, stable-key sets, and freshness hints,
// so a redelivered generation stays idempotent.
func TestScanParityDuplicateDeliveryIsStable(t *testing.T) {
	t.Parallel()

	first := drainResults(t, parityLiveSource(t))
	second := drainResults(t, parityLiveSource(t))
	if !slices.Equal(stableKeysOf(first), stableKeysOf(second)) {
		t.Fatal("duplicate scans emit different stable keys; redelivery would not be idempotent")
	}
	for i := range first {
		if first[i].Generation.ID != second[i].Generation.ID {
			t.Fatalf("scope %d generation differs on rescan", i)
		}
		if first[i].Generation.FreshnessHint != second[i].Generation.FreshnessHint {
			t.Fatalf("scope %d freshness hint differs on rescan", i)
		}
	}
}

// TestScanParityEmptySuccessorAttestation pins the TrustedDeletion half of
// empty-successor handling: an emptied repository still emits its repository
// fact, and only an explicit attestation stamps the generation trusted.
func TestScanParityEmptySuccessorAttestation(t *testing.T) {
	t.Parallel()

	scan := func(allowEmpty bool) ScanResult {
		t.Helper()
		source := &Source{
			Config: Config{
				CollectorInstanceID: "oci-parity-8861",
				Targets: []TargetConfig{{
					Provider:            oci.ProviderDockerHub,
					Registry:            "registry-1.docker.io",
					Repository:          "library/emptied",
					TagLimit:            10,
					SourceURI:           "https://registry-1.docker.io/v2/library/emptied",
					FencingToken:        7,
					AllowEmptySuccessor: allowEmpty,
				}},
			},
			ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
				return &stubRegistryClient{}, nil
			}),
			Clock: func() time.Time { return parityObservedAt },
		}
		result, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			t.Fatal("Next() ok = false, want the emptied scope")
		}
		return result
	}

	plain := scan(false)
	if plain.Generation.TrustedDeletion {
		t.Fatal("TrustedDeletion = true without attestation, want false")
	}
	kinds := map[string]int{}
	for _, fact := range plain.Facts {
		kinds[fact.Kind]++
	}
	if kinds[oci.FactKindRepository] != 1 {
		t.Fatalf("emptied scope kinds = %v, want exactly the repository fact", kinds)
	}
	if attested := scan(true); !attested.Generation.TrustedDeletion {
		t.Fatal("TrustedDeletion = false with AllowEmptySuccessor, want true")
	}
}

// TestScanClaimReusesGenerationAndFencingToken pins the claim path: the scan
// resolves the claimed scope, reuses the claim generation ID so retries are
// idempotent, and stamps the claim fencing token on the scan.
func TestScanClaimReusesGenerationAndFencingToken(t *testing.T) {
	t.Parallel()

	source := parityLiveSource(t)
	const scopeID = "oci-registry://registry-1.docker.io/library/busybox"
	result, ok, err := source.ScanClaim(context.Background(), scopeID, "generation-claim-9", 42)
	if err != nil {
		t.Fatalf("ScanClaim() error = %v", err)
	}
	if !ok {
		t.Fatal("ScanClaim() ok = false, want the claimed scope")
	}
	if result.ScopeID != scopeID {
		t.Fatalf("ScopeID = %q, want %q", result.ScopeID, scopeID)
	}
	if result.Generation.ID != "generation-claim-9" {
		t.Fatalf("GenerationID = %q, want the claim generation", result.Generation.ID)
	}
	if len(result.Facts) == 0 {
		t.Fatal("claimed scan has no facts")
	}
	for _, fact := range result.Facts {
		if fact.SourceRef.GenerationID != "generation-claim-9" {
			t.Fatalf("fact %q carries %q, want the claim generation", fact.Kind, fact.SourceRef.GenerationID)
		}
	}

	if _, ok, err := source.ScanClaim(context.Background(), "oci-registry://example.invalid/unknown", "generation-x", 0); err != nil || ok {
		t.Fatalf("ScanClaim(unknown) = (%v, %v), want (false, nil)", ok, err)
	}
}
