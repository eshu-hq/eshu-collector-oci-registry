// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

const (
	parityDigest       = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	parityConfigDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	parityLayerDigest  = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	paritySubject      = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	parityReferrer     = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

var parityObservedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func parityRepository() oci.RepositoryIdentity {
	return oci.RepositoryIdentity{
		Provider:   oci.ProviderDockerHub,
		Registry:   "registry-1.docker.io",
		Repository: "library/busybox",
	}
}

// parityFacts builds one fact per constructor with fixed inputs. The
// generation and scope IDs are the claim boundary the handler propagates.
func parityFacts(t *testing.T) []sdk.Fact {
	t.Helper()

	repository, err := oci.NewRepositoryEnvelope(oci.RepositoryObservation{
		Identity:            parityRepository(),
		GenerationID:        "generation-parity",
		CollectorInstanceID: "oci-parity",
		FencingToken:        7,
		ObservedAt:          parityObservedAt,
		Visibility:          oci.VisibilityPublic,
		AuthMode:            oci.AuthModeAnonymous,
		SourceURI:           "https://registry-1.docker.io/v2/library/busybox",
	})
	if err != nil {
		t.Fatalf("NewRepositoryEnvelope() error = %v", err)
	}
	tag, err := oci.NewTagObservationEnvelope(oci.TagObservation{
		Repository:          parityRepository(),
		Tag:                 "latest",
		Digest:              parityDigest,
		MediaType:           oci.MediaTypeOCIImageManifest,
		GenerationID:        "generation-parity",
		CollectorInstanceID: "oci-parity",
		FencingToken:        7,
		ObservedAt:          parityObservedAt,
		SourceURI:           "https://registry-1.docker.io/v2/library/busybox",
	})
	if err != nil {
		t.Fatalf("NewTagObservationEnvelope() error = %v", err)
	}
	manifest, err := oci.NewManifestEnvelope(oci.ManifestObservation{
		Repository: parityRepository(),
		Descriptor: oci.Descriptor{
			Digest:    parityDigest,
			MediaType: oci.MediaTypeOCIImageManifest,
			SizeBytes: 512,
		},
		Config: oci.Descriptor{
			Digest:    parityConfigDigest,
			MediaType: "application/vnd.oci.image.config.v1+json",
			SizeBytes: 256,
		},
		Layers: []oci.Descriptor{{
			Digest:    parityLayerDigest,
			MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
			SizeBytes: 1024,
		}},
		SourceTag:           "latest",
		GenerationID:        "generation-parity",
		CollectorInstanceID: "oci-parity",
		FencingToken:        7,
		ObservedAt:          parityObservedAt,
		SourceURI:           "https://registry-1.docker.io/v2/library/busybox",
	})
	if err != nil {
		t.Fatalf("NewManifestEnvelope() error = %v", err)
	}
	index, err := oci.NewImageIndexEnvelope(oci.IndexObservation{
		Repository: parityRepository(),
		Descriptor: oci.Descriptor{
			Digest:    parityReferrer,
			MediaType: oci.MediaTypeOCIImageIndex,
			SizeBytes: 1024,
		},
		Manifests: []oci.Descriptor{{
			Digest:       parityDigest,
			MediaType:    oci.MediaTypeOCIImageManifest,
			SizeBytes:    512,
			Platform:     oci.Platform{OS: "linux", Architecture: "amd64"},
			ArtifactType: "",
		}},
		GenerationID:        "generation-parity",
		CollectorInstanceID: "oci-parity",
		FencingToken:        7,
		ObservedAt:          parityObservedAt,
		SourceURI:           "https://registry-1.docker.io/v2/library/busybox",
	})
	if err != nil {
		t.Fatalf("NewImageIndexEnvelope() error = %v", err)
	}
	descriptor, err := oci.NewDescriptorEnvelope(oci.DescriptorObservation{
		Repository: parityRepository(),
		Descriptor: oci.Descriptor{
			Digest:    parityLayerDigest,
			MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
			SizeBytes: 1024,
		},
		GenerationID:        "generation-parity",
		CollectorInstanceID: "oci-parity",
		FencingToken:        7,
		ObservedAt:          parityObservedAt,
		SourceURI:           "https://registry-1.docker.io/v2/library/busybox",
	})
	if err != nil {
		t.Fatalf("NewDescriptorEnvelope() error = %v", err)
	}
	referrer, err := oci.NewReferrerEnvelope(oci.ReferrerObservation{
		Repository: parityRepository(),
		Subject: oci.Descriptor{
			Digest:    paritySubject,
			MediaType: oci.MediaTypeOCIImageManifest,
		},
		Referrer: oci.Descriptor{
			Digest:       parityReferrer,
			MediaType:    oci.MediaTypeOCIImageManifest,
			SizeBytes:    128,
			ArtifactType: "application/vnd.in-toto+json",
		},
		SourceAPIPath:       "/v2/<repository>/referrers/<digest>",
		GenerationID:        "generation-parity",
		CollectorInstanceID: "oci-parity",
		FencingToken:        7,
		ObservedAt:          parityObservedAt,
		SourceURI:           "https://registry-1.docker.io/v2/library/busybox",
	})
	if err != nil {
		t.Fatalf("NewReferrerEnvelope() error = %v", err)
	}
	warning, err := oci.NewWarningEnvelope(oci.WarningObservation{
		WarningKey:          "unsupported_referrers_api:referrers API not supported by this registry",
		WarningCode:         oci.WarningUnsupportedReferrersAPI,
		Severity:            oci.SeverityInfo,
		Message:             "referrers API not supported by this registry",
		Repository:          &oci.RepositoryIdentity{Provider: oci.ProviderDockerHub, Registry: "registry-1.docker.io", Repository: "library/busybox"},
		Digest:              parityDigest,
		GenerationID:        "generation-parity",
		CollectorInstanceID: "oci-parity",
		FencingToken:        7,
		ObservedAt:          parityObservedAt,
		SourceURI:           "https://registry-1.docker.io/v2/library/busybox",
	})
	if err != nil {
		t.Fatalf("NewWarningEnvelope() error = %v", err)
	}
	return []sdk.Fact{repository, tag, manifest, index, descriptor, referrer, warning}
}

// TestConstructorsAdmitAllSevenKindsAtOnePointOh proves the ported constructors
// admit the same 7 fact kinds at schema 1.0.0 with reported confidence: every
// kind the contract and manifest declare has a constructor that emits it.
func TestConstructorsAdmitAllSevenKindsAtOnePointOh(t *testing.T) {
	t.Parallel()

	facts := parityFacts(t)
	if len(facts) != len(FactKinds()) {
		t.Fatalf("parity facts = %d, want %d", len(facts), len(FactKinds()))
	}
	seen := map[string]bool{}
	for _, fact := range facts {
		if fact.SchemaVersion != SchemaVersion {
			t.Fatalf("fact %q SchemaVersion = %q, want %q", fact.Kind, fact.SchemaVersion, SchemaVersion)
		}
		if fact.SourceConfidence != sdk.SourceConfidenceReported {
			t.Fatalf("fact %q SourceConfidence = %q, want reported", fact.Kind, fact.SourceConfidence)
		}
		if seen[fact.Kind] {
			t.Fatalf("duplicate kind %q", fact.Kind)
		}
		seen[fact.Kind] = true
	}
	for _, kind := range FactKinds() {
		if !seen[kind] {
			t.Fatalf("no constructor emits %q", kind)
		}
	}
}

// TestConstructorStableKeysAreDeterministicAndDistinct proves stable keys are
// fit to key idempotent commits: rebuilding the same observation reproduces
// the key, and different observations never share one.
func TestConstructorStableKeysAreDeterministicAndDistinct(t *testing.T) {
	t.Parallel()

	first := parityFacts(t)
	second := parityFacts(t)
	keys := map[string]string{}
	for i, fact := range first {
		if fact.StableKey == "" {
			t.Fatalf("fact %q has a blank stable key", fact.Kind)
		}
		if fact.StableKey != second[i].StableKey {
			t.Fatalf("fact %q stable key changed on rebuild: %q != %q", fact.Kind, fact.StableKey, second[i].StableKey)
		}
		if other, dup := keys[fact.StableKey]; dup {
			t.Fatalf("facts %q and %q share stable key %q", other, fact.Kind, fact.StableKey)
		}
		keys[fact.StableKey] = fact.Kind
	}
}

// TestConstructorFactsPropagateScopeAndGeneration proves every constructor
// carries the claim boundary into SourceRef: the host keys admission, grants,
// and commits on scope and generation, never on payload inspection.
func TestConstructorFactsPropagateScopeAndGeneration(t *testing.T) {
	t.Parallel()

	const scopeID = "oci-registry://registry-1.docker.io/library/busybox"
	for _, fact := range parityFacts(t) {
		if fact.SourceRef.ScopeID != scopeID {
			t.Fatalf("fact %q SourceRef.ScopeID = %q, want %q", fact.Kind, fact.SourceRef.ScopeID, scopeID)
		}
		if fact.SourceRef.GenerationID != "generation-parity" {
			t.Fatalf("fact %q SourceRef.GenerationID = %q, want generation-parity", fact.Kind, fact.SourceRef.GenerationID)
		}
		if fact.SourceRef.SourceSystem != SourceSystem {
			t.Fatalf("fact %q SourceRef.SourceSystem = %q, want %q", fact.Kind, fact.SourceRef.SourceSystem, SourceSystem)
		}
		if !fact.ObservedAt.Equal(parityObservedAt) {
			t.Fatalf("fact %q ObservedAt = %s, want %s", fact.Kind, fact.ObservedAt, parityObservedAt)
		}
	}
}

// TestContractDeclaresEveryManifestKind keeps the SDK contract, the fact-kind
// vocabulary, and the shipped manifest in step.
func TestContractDeclaresEveryManifestKind(t *testing.T) {
	t.Parallel()

	contract := Contract()
	if contract.ProtocolVersion != sdk.ProtocolVersionV1Alpha1 {
		t.Fatalf("ProtocolVersion = %q, want %q", contract.ProtocolVersion, sdk.ProtocolVersionV1Alpha1)
	}
	declared := map[string]sdk.FactDeclaration{}
	for _, family := range contract.Facts {
		declared[family.Kind] = family
	}
	for _, kind := range FactKinds() {
		family, ok := declared[kind]
		if !ok {
			t.Fatalf("contract omits %q", kind)
		}
		if len(family.SchemaVersions) != 1 || family.SchemaVersions[0] != SchemaVersion {
			t.Fatalf("contract %q SchemaVersions = %v, want [%s]", kind, family.SchemaVersions, SchemaVersion)
		}
		if len(family.SourceConfidence) != 1 || family.SourceConfidence[0] != sdk.SourceConfidenceReported {
			t.Fatalf("contract %q SourceConfidence = %v, want [reported]", kind, family.SourceConfidence)
		}
	}
	manifest := loadManifest(t)
	if len(manifest.Spec.EmittedFacts) != len(FactKinds()) {
		t.Fatalf("manifest declares %d kinds, want %d", len(manifest.Spec.EmittedFacts), len(FactKinds()))
	}
	for i, family := range manifest.Spec.EmittedFacts {
		if family.Kind != FactKinds()[i] {
			t.Fatalf("manifest kind[%d] = %q, want %q", i, family.Kind, FactKinds()[i])
		}
	}
}
