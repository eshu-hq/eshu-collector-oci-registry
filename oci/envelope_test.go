// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oci

import (
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

func TestRepositoryObservationBuildsReportedRepositoryEnvelope(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	observation := RepositoryObservation{
		Identity: RepositoryIdentity{
			Provider:   ProviderECR,
			Registry:   "https://123456789012.dkr.ecr.us-east-1.amazonaws.com/",
			Repository: "Team/API-Service",
		},
		GenerationID:        "generation-1",
		CollectorInstanceID: "ecr-prod",
		FencingToken:        17,
		ObservedAt:          observedAt,
		Visibility:          VisibilityPrivate,
		AuthMode:            AuthModeCredentialed,
		SourceURI:           "https://user:secret@123456789012.dkr.ecr.us-east-1.amazonaws.com/v2/team/api-service?token=secret",
	}

	fact, err := NewRepositoryEnvelope(observation)
	if err != nil {
		t.Fatalf("NewRepositoryEnvelope() error = %v", err)
	}

	assertOCIFact(t, fact, FactKindRepository, SchemaVersion)
	if fact.SourceRef.ScopeID != "oci-registry://123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api-service" {
		t.Fatalf("SourceRef.ScopeID = %q", fact.SourceRef.ScopeID)
	}
	if fact.SourceRef.GenerationID != "generation-1" {
		t.Fatalf("SourceRef.GenerationID = %q, want %q", fact.SourceRef.GenerationID, "generation-1")
	}
	if !fact.ObservedAt.Equal(observedAt) {
		t.Fatalf("ObservedAt = %s, want %s", fact.ObservedAt, observedAt)
	}
	if fact.SourceRef.URI != "https://123456789012.dkr.ecr.us-east-1.amazonaws.com/v2/team/api-service" {
		t.Fatalf("SourceRef.SourceURI = %q", fact.SourceRef.URI)
	}

	wantPayload := map[string]any{
		"collector_instance_id": "ecr-prod",
		"provider":              string(ProviderECR),
		"registry":              "123456789012.dkr.ecr.us-east-1.amazonaws.com",
		"repository":            "team/api-service",
		"repository_id":         "oci-registry://123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api-service",
		"visibility":            string(VisibilityPrivate),
		"auth_mode":             string(AuthModeCredentialed),
	}
	for key, want := range wantPayload {
		if got := fact.Payload[key]; got != want {
			t.Fatalf("Payload[%q] = %#v, want %#v; payload=%#v", key, got, want, fact.Payload)
		}
	}
	assertStringSlice(t, fact.Payload["correlation_anchors"], []string{
		"oci-registry://123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api-service",
	})
}

func TestTagObservationBuildsWeakTagEnvelopeWithDigestIdentity(t *testing.T) {
	t.Parallel()

	observation := TagObservation{
		Repository: RepositoryIdentity{
			Provider:   ProviderJFrog,
			Registry:   "https://jfrog.example/artifactory/api/docker/prod",
			Repository: "team/api",
		},
		Tag:                 "Latest",
		Digest:              sha256Digest,
		MediaType:           MediaTypeOCIImageManifest,
		PreviousDigest:      "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Mutated:             true,
		GenerationID:        "generation-tags",
		CollectorInstanceID: "jfrog-docker",
		ObservedAt:          time.Date(2026, 5, 12, 12, 5, 0, 0, time.UTC),
		SourceURI:           "https://jfrog.example/artifactory/api/docker/prod/v2/team/api/tags/list?access_token=secret",
	}

	fact, err := NewTagObservationEnvelope(observation)
	if err != nil {
		t.Fatalf("NewTagObservationEnvelope() error = %v", err)
	}

	assertOCIFact(t, fact, FactKindTagObservation, SchemaVersion)
	if got := fact.Payload["tag"]; got != "Latest" {
		t.Fatalf("tag = %#v", got)
	}
	if got := fact.Payload["resolved_digest"]; got != sha256Digest {
		t.Fatalf("resolved_digest = %#v", got)
	}
	if got := fact.Payload["identity_strength"]; got != IdentityStrengthWeakTag {
		t.Fatalf("identity_strength = %#v", got)
	}
	if got := fact.Payload["mutated"]; got != true {
		t.Fatalf("mutated = %#v", got)
	}
	if fact.SourceRef.URI != "https://jfrog.example/artifactory/api/docker/prod/v2/team/api/tags/list" {
		t.Fatalf("SourceRef.SourceURI = %q", fact.SourceRef.URI)
	}
	assertStringSlice(t, fact.Payload["correlation_anchors"], []string{
		"oci-registry://jfrog.example/artifactory/api/docker/prod/team/api",
		sha256Digest,
	})

	sameObservation := observation
	sameObservation.Repository.Registry = "jfrog.example/artifactory/api/docker/prod/"
	second, err := NewTagObservationEnvelope(sameObservation)
	if err != nil {
		t.Fatalf("NewTagObservationEnvelope(sameObservation) error = %v", err)
	}
	if fact.StableKey != second.StableKey {
		t.Fatalf("StableFactKey differs for normalized repository identity: %q != %q", fact.StableKey, second.StableKey)
	}

	nextGeneration := observation
	nextGeneration.GenerationID = "generation-tags-next"
	third, err := NewTagObservationEnvelope(nextGeneration)
	if err != nil {
		t.Fatalf("NewTagObservationEnvelope(nextGeneration) error = %v", err)
	}
	if fact.StableKey != third.StableKey {
		t.Fatalf("StableFactKey changed across generations: %q != %q", fact.StableKey, third.StableKey)
	}
	if fact.SourceRef.GenerationID == third.SourceRef.GenerationID {
		t.Fatalf("SourceRef.GenerationID did not carry the generation boundary: %q", fact.SourceRef.GenerationID)
	}
}

func TestDescriptorEnvelopesPreserveDigestIdentityAndRedactUnknownAnnotations(t *testing.T) {
	t.Parallel()

	observation := ManifestObservation{
		Repository: RepositoryIdentity{
			Provider:   ProviderECR,
			Registry:   "123456789012.dkr.ecr.us-east-1.amazonaws.com",
			Repository: "team/api",
		},
		Descriptor: Descriptor{
			Digest:       sha256Digest,
			MediaType:    MediaTypeOCIImageManifest,
			SizeBytes:    2048,
			ArtifactType: "application/vnd.example.scan",
			Annotations: map[string]string{
				"org.opencontainers.image.source":  "https://github.com/example/api",
				"com.example.private.build.secret": "do-not-leak",
			},
		},
		Config: Descriptor{
			Digest:    "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			MediaType: "application/vnd.oci.image.config.v1+json",
			SizeBytes: 512,
		},
		Layers: []Descriptor{{
			Digest:    "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
			SizeBytes: 4096,
		}},
		SourceTag:           "latest",
		GenerationID:        "generation-manifest",
		CollectorInstanceID: "ecr-prod",
		ObservedAt:          time.Date(2026, 5, 12, 12, 10, 0, 0, time.UTC),
		SourceURI:           "https://123456789012.dkr.ecr.us-east-1.amazonaws.com/v2/team/api/manifests/latest",
	}

	fact, err := NewManifestEnvelope(observation)
	if err != nil {
		t.Fatalf("NewManifestEnvelope() error = %v", err)
	}

	assertOCIFact(t, fact, FactKindManifest, SchemaVersion)
	if got := fact.Payload["digest"]; got != sha256Digest {
		t.Fatalf("digest = %#v", got)
	}
	if got := fact.Payload["source_tag"]; got != "latest" {
		t.Fatalf("source_tag = %#v", got)
	}
	annotations, ok := fact.Payload["annotations"].(map[string]string)
	if !ok {
		t.Fatalf("annotations = %#v", fact.Payload["annotations"])
	}
	if got := annotations["org.opencontainers.image.source"]; got != "https://github.com/example/api" {
		t.Fatalf("known annotation = %q", got)
	}
	if got := annotations["com.example.private.build.secret"]; got != RedactedValue {
		t.Fatalf("unknown annotation = %q, want redacted", got)
	}

	sameDigestDifferentTag := observation
	sameDigestDifferentTag.SourceTag = "release-2026-05-12"
	second, err := NewManifestEnvelope(sameDigestDifferentTag)
	if err != nil {
		t.Fatalf("NewManifestEnvelope(sameDigestDifferentTag) error = %v", err)
	}
	if fact.StableKey != second.StableKey {
		t.Fatalf("StableFactKey included mutable tag: %q != %q", fact.StableKey, second.StableKey)
	}
}

func TestIndexDescriptorAndReferrerEnvelopeBuilders(t *testing.T) {
	t.Parallel()

	repository := RepositoryIdentity{
		Provider:   ProviderJFrog,
		Registry:   "jfrog.example/artifactory/api/docker/prod",
		Repository: "team/api",
	}
	indexDigest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	manifestDigest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	attestationDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	indexFact, err := NewImageIndexEnvelope(IndexObservation{
		Repository: repository,
		Descriptor: Descriptor{
			Digest:    indexDigest,
			MediaType: MediaTypeOCIImageIndex,
			SizeBytes: 1024,
		},
		Manifests: []Descriptor{{
			Digest:    manifestDigest,
			MediaType: MediaTypeOCIImageManifest,
			SizeBytes: 2048,
			Platform:  Platform{OS: "linux", Architecture: "amd64"},
		}},
		GenerationID:        "generation-index",
		CollectorInstanceID: "jfrog-docker",
	})
	if err != nil {
		t.Fatalf("NewImageIndexEnvelope() error = %v", err)
	}
	assertOCIFact(t, indexFact, FactKindIndex, SchemaVersion)

	descriptorFact, err := NewDescriptorEnvelope(DescriptorObservation{
		Repository: repository,
		Descriptor: Descriptor{
			Digest:       attestationDigest,
			MediaType:    "application/vnd.in-toto+json",
			SizeBytes:    333,
			ArtifactType: "application/vnd.in-toto+json",
		},
		GenerationID:        "generation-descriptor",
		CollectorInstanceID: "jfrog-docker",
	})
	if err != nil {
		t.Fatalf("NewDescriptorEnvelope() error = %v", err)
	}
	assertOCIFact(t, descriptorFact, FactKindDescriptor, SchemaVersion)

	referrerFact, err := NewReferrerEnvelope(ReferrerObservation{
		Repository: repository,
		Subject: Descriptor{
			Digest:    manifestDigest,
			MediaType: MediaTypeOCIImageManifest,
			SizeBytes: 2048,
		},
		Referrer: Descriptor{
			Digest:       attestationDigest,
			MediaType:    "application/vnd.in-toto+json",
			SizeBytes:    333,
			ArtifactType: "application/vnd.in-toto+json",
		},
		SourceAPIPath:       "/v2/team/api/referrers/" + manifestDigest,
		GenerationID:        "generation-referrer",
		CollectorInstanceID: "jfrog-docker",
	})
	if err != nil {
		t.Fatalf("NewReferrerEnvelope() error = %v", err)
	}
	assertOCIFact(t, referrerFact, FactKindReferrer, SchemaVersion)
	if got := referrerFact.Payload["subject_digest"]; got != manifestDigest {
		t.Fatalf("subject_digest = %#v", got)
	}
	if got := referrerFact.Payload["referrer_digest"]; got != attestationDigest {
		t.Fatalf("referrer_digest = %#v", got)
	}
}

func assertOCIFact(t *testing.T, fact sdk.Fact, factKind, schemaVersion string) {
	t.Helper()

	if fact.Kind != factKind {
		t.Fatalf("Kind = %q, want %q", fact.Kind, factKind)
	}
	if fact.SchemaVersion != schemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", fact.SchemaVersion, schemaVersion)
	}
	if fact.SourceConfidence != sdk.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", fact.SourceConfidence, sdk.SourceConfidenceReported)
	}
	if fact.SourceRef.SourceSystem != CollectorKind {
		t.Fatalf("SourceRef.SourceSystem = %q, want %q", fact.SourceRef.SourceSystem, CollectorKind)
	}
	if fact.StableKey == "" {
		t.Fatalf("stable key must not be blank: %#v", fact)
	}
}

func assertStringSlice(t *testing.T, got any, want []string) {
	t.Helper()

	values, ok := got.([]string)
	if !ok {
		t.Fatalf("value = %#v, want []string", got)
	}
	if len(values) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(values), len(want), values)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("values[%d] = %q, want %q; values=%#v", i, values[i], want[i], values)
		}
	}
}
