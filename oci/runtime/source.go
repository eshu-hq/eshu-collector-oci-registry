// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu-collector-oci-registry/internal/stableid"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/distribution"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// ScopeKindContainerRegistryRepository is the scope kind of an OCI registry
// repository scan. The host owns scope admission; the collector echoes it.
const ScopeKindContainerRegistryRepository = "container_registry_repository"

// RegistryClient is the narrow OCI Distribution contract used by the runtime.
type RegistryClient interface {
	Ping(context.Context) error
	ListTags(context.Context, string, int) (distribution.TagListResponse, error)
	GetManifest(context.Context, string, string) (distribution.ManifestResponse, error)
	GetBlob(context.Context, string, string) (distribution.BlobResponse, error)
	ListReferrers(context.Context, string, string) (distribution.ReferrersResponse, error)
}

// ClientFactory creates a registry client for one configured target.
type ClientFactory interface {
	Client(context.Context, TargetConfig) (RegistryClient, error)
}

// ClientFactoryFunc adapts a function into a ClientFactory.
type ClientFactoryFunc func(context.Context, TargetConfig) (RegistryClient, error)

// Client creates a registry client.
func (f ClientFactoryFunc) Client(ctx context.Context, target TargetConfig) (RegistryClient, error) {
	return f(ctx, target)
}

// Generation identifies one repository scan for the claimed scope.
type Generation struct {
	ID              string
	ObservedAt      time.Time
	FreshnessHint   string
	TrustedDeletion bool
}

// ScanResult is one repository scan: the scope and generation the facts
// belong to plus the emitted SDK facts in emission order.
type ScanResult struct {
	ScopeID    string
	ScopeKind  string
	Generation Generation
	Facts      []sdk.Fact
}

// Source scans configured OCI registry repositories and yields generations.
type Source struct {
	Config        Config
	ClientFactory ClientFactory
	Logger        *slog.Logger
	Clock         func() time.Time

	next int
	// transportFailures counts consecutive transient failures (transport or
	// retryable HTTP status) per target index; a successful scan of the
	// target clears its entry.
	transportFailures map[int]int
}

// Next returns the next configured registry repository scan result.
func (s *Source) Next(ctx context.Context) (ScanResult, bool, error) {
	config, err := s.Config.validated()
	if err != nil {
		return ScanResult{}, false, err
	}
	if s.ClientFactory == nil {
		return ScanResult{}, false, fmt.Errorf("OCI registry client factory is required")
	}
	// Walk forward past targets that hit a transient error (transport or
	// retryable HTTP status) so one failed target does not report the batch
	// as drained or delay every later target by a poll interval.
	for s.next < len(config.Targets) {
		index := s.next
		target := config.Targets[index]
		s.next++
		collected, err := s.scanTarget(ctx, config, target, "")
		if err == nil {
			delete(s.transportFailures, index)
			return collected, true, nil
		}
		if !isSkippableTransientError(ctx, err) {
			return ScanResult{}, false, err
		}
		if err := s.skipTransientTransport(ctx, index, target, err); err != nil {
			return ScanResult{}, false, err
		}
	}
	s.next = 0
	return ScanResult{}, false, nil
}

func (s *Source) scanTarget(ctx context.Context, config Config, target TargetConfig, generationID string) (ScanResult, error) {
	if err := ctx.Err(); err != nil {
		return ScanResult{}, err
	}
	start := time.Now()

	client, err := s.ClientFactory.Client(ctx, target)
	if err != nil {
		return ScanResult{}, fmt.Errorf("create OCI registry client: %w", err)
	}
	if err := client.Ping(ctx); err != nil {
		return ScanResult{}, fmt.Errorf("ping OCI registry: %w", err)
	}
	tags, tagListTruncated, err := s.listReferences(ctx, client, target)
	if err != nil {
		return ScanResult{}, err
	}

	observedAt := s.now()
	scopeID, generation, err := s.scopeAndGeneration(target, observedAt, tags, generationID)
	if err != nil {
		return ScanResult{}, err
	}
	facts, refDigests, err := s.buildFacts(ctx, client, target, config.CollectorInstanceID, generation.ID, observedAt, tags)
	if err != nil {
		return ScanResult{}, err
	}
	if tagListTruncated {
		warning, warningErr := s.warningFact(
			target,
			config.CollectorInstanceID,
			generation.ID,
			observedAt,
			oci.WarningTagListTruncated,
			"repository tag list exceeded the configured bounded collection window",
			"",
		)
		if warningErr != nil {
			return ScanResult{}, warningErr
		}
		facts = append(facts, warning)
	}
	// Digests exist only after buildFacts fetches manifests, so the
	// content hint is attached post-scan (#8003).
	generation.FreshnessHint = ociScanFreshnessHint(refDigests, facts, tagListTruncated)
	if s.Logger != nil {
		s.Logger.InfoContext(
			ctx, "OCI registry scan completed",
			slog.String("phase", "discovery"),
			slog.String("scope_id", scopeID),
			slog.String("generation_id", generation.ID),
			slog.String("provider", string(target.Provider)),
			slog.String("tls_mode", string(target.TLSMode())),
			slog.Int("reference_count", len(tags)),
			slog.Int("fact_count", len(facts)),
			slog.Float64("duration_seconds", time.Since(start).Seconds()),
		)
	}
	return ScanResult{
		ScopeID:    scopeID,
		ScopeKind:  ScopeKindContainerRegistryRepository,
		Generation: generation,
		Facts:      facts,
	}, nil
}

func (s *Source) buildFacts(
	ctx context.Context,
	client RegistryClient,
	target TargetConfig,
	collectorInstanceID string,
	generationID string,
	observedAt time.Time,
	references []string,
) ([]sdk.Fact, []ociReferenceDigest, error) {
	facts := make([]sdk.Fact, 0, 1+(len(references)*4))
	refDigests := make([]ociReferenceDigest, 0, len(references))
	repositoryFact, err := oci.NewRepositoryEnvelope(oci.RepositoryObservation{
		Identity:            target.identity(),
		GenerationID:        generationID,
		CollectorInstanceID: collectorInstanceID,
		FencingToken:        target.FencingToken,
		ObservedAt:          observedAt,
		Visibility:          target.Visibility,
		AuthMode:            target.AuthMode,
		SourceURI:           target.SourceURI,
	})
	if err != nil {
		return nil, nil, err
	}
	facts = append(facts, repositoryFact)

	for _, reference := range references {
		manifest, err := client.GetManifest(ctx, target.Repository, reference)
		if err != nil {
			return nil, nil, fmt.Errorf("get OCI manifest: %w", err)
		}
		mediaType, parsed, err := parseManifest(manifest)
		if err != nil {
			return nil, nil, fmt.Errorf("parse OCI manifest: %w", err)
		}
		digest, digestWarning, ok := manifestDigest(manifest)
		if !ok {
			warning, warningErr := s.warningFact(target, collectorInstanceID, generationID, observedAt, oci.WarningMissingManifestDigest, reference, "")
			if warningErr != nil {
				return nil, nil, warningErr
			}
			facts = append(facts, warning)
			refDigests = append(refDigests, ociReferenceDigest{reference: reference, digestWarning: oci.WarningMissingManifestDigest})
			continue
		}
		refDigests = append(refDigests, ociReferenceDigest{reference: reference, digest: digest, digestWarning: digestWarning})
		if digestWarning != "" {
			warning, warningErr := s.warningFact(target, collectorInstanceID, generationID, observedAt, digestWarning, reference, digest)
			if warningErr != nil {
				return nil, nil, warningErr
			}
			facts = append(facts, warning)
		}

		descriptor := oci.Descriptor{Digest: digest, MediaType: mediaType, SizeBytes: manifest.SizeBytes}
		configLabels, configWarnings, err := s.configProvenanceLabels(
			ctx,
			client,
			target,
			collectorInstanceID,
			generationID,
			observedAt,
			parsed.Config,
		)
		if err != nil {
			return nil, nil, err
		}
		parsed.ConfigLabels = configLabels
		facts = append(facts, configWarnings...)
		tagFact, err := oci.NewTagObservationEnvelope(oci.TagObservation{
			Repository:          target.identity(),
			Tag:                 reference,
			Digest:              digest,
			MediaType:           mediaType,
			GenerationID:        generationID,
			CollectorInstanceID: collectorInstanceID,
			FencingToken:        target.FencingToken,
			ObservedAt:          observedAt,
			SourceURI:           target.SourceURI,
		})
		if err != nil {
			return nil, nil, err
		}
		facts = append(facts, tagFact)

		factsForManifest, err := s.manifestFacts(target, collectorInstanceID, generationID, observedAt, reference, descriptor, parsed)
		if err != nil {
			return nil, nil, err
		}
		facts = append(facts, factsForManifest...)
		referrerFacts, err := s.referrerFacts(ctx, client, target, collectorInstanceID, generationID, observedAt, descriptor)
		if err != nil {
			return nil, nil, err
		}
		facts = append(facts, referrerFacts...)
	}
	return facts, refDigests, nil
}

func (s *Source) manifestFacts(
	target TargetConfig,
	collectorInstanceID string,
	generationID string,
	observedAt time.Time,
	sourceTag string,
	descriptor oci.Descriptor,
	parsed parsedManifest,
) ([]sdk.Fact, error) {
	switch mediaFamily(descriptor.MediaType) {
	case "image_index":
		fact, err := oci.NewImageIndexEnvelope(oci.IndexObservation{
			Repository:          target.identity(),
			Descriptor:          descriptor,
			Manifests:           parsed.Manifests,
			GenerationID:        generationID,
			CollectorInstanceID: collectorInstanceID,
			FencingToken:        target.FencingToken,
			ObservedAt:          observedAt,
			SourceURI:           target.SourceURI,
		})
		return []sdk.Fact{fact}, err
	default:
		facts := make([]sdk.Fact, 0, 2+len(parsed.Layers))
		manifestFact, err := oci.NewManifestEnvelope(oci.ManifestObservation{
			Repository:          target.identity(),
			Descriptor:          descriptor,
			Config:              parsed.Config,
			ConfigLabels:        parsed.ConfigLabels,
			Layers:              parsed.Layers,
			SourceTag:           sourceTag,
			GenerationID:        generationID,
			CollectorInstanceID: collectorInstanceID,
			FencingToken:        target.FencingToken,
			ObservedAt:          observedAt,
			SourceURI:           target.SourceURI,
		})
		if err != nil {
			return nil, err
		}
		facts = append(facts, manifestFact)
		for _, child := range append([]oci.Descriptor{parsed.Config}, parsed.Layers...) {
			if strings.TrimSpace(child.Digest) == "" {
				continue
			}
			childFact, err := oci.NewDescriptorEnvelope(oci.DescriptorObservation{
				Repository:          target.identity(),
				Descriptor:          child,
				GenerationID:        generationID,
				CollectorInstanceID: collectorInstanceID,
				FencingToken:        target.FencingToken,
				ObservedAt:          observedAt,
				SourceURI:           target.SourceURI,
			})
			if err != nil {
				return nil, err
			}
			facts = append(facts, childFact)
		}
		return facts, nil
	}
}

func (s *Source) referrerFacts(
	ctx context.Context,
	client RegistryClient,
	target TargetConfig,
	collectorInstanceID string,
	generationID string,
	observedAt time.Time,
	subject oci.Descriptor,
) ([]sdk.Fact, error) {
	referrers, err := client.ListReferrers(ctx, target.Repository, subject.Digest)
	if err != nil {
		warning, warningErr := s.warningFact(target, collectorInstanceID, generationID, observedAt, oci.WarningUnsupportedReferrersAPI, err.Error(), subject.Digest)
		if warningErr != nil {
			return nil, warningErr
		}
		return []sdk.Fact{warning}, nil
	}
	facts := make([]sdk.Fact, 0, len(referrers.Referrers))
	for _, referrer := range referrers.Referrers {
		fact, err := oci.NewReferrerEnvelope(oci.ReferrerObservation{
			Repository:          target.identity(),
			Subject:             subject,
			Referrer:            referrer,
			SourceAPIPath:       "/v2/<repository>/referrers/<digest>",
			GenerationID:        generationID,
			CollectorInstanceID: collectorInstanceID,
			FencingToken:        target.FencingToken,
			ObservedAt:          observedAt,
			SourceURI:           target.SourceURI,
		})
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func (s *Source) warningFact(
	target TargetConfig,
	collectorInstanceID string,
	generationID string,
	observedAt time.Time,
	code string,
	message string,
	digest string,
) (sdk.Fact, error) {
	repository := target.identity()
	return oci.NewWarningEnvelope(oci.WarningObservation{
		WarningKey:          code + ":" + message,
		WarningCode:         code,
		Severity:            oci.SeverityInfo,
		Message:             message,
		Repository:          &repository,
		Digest:              digest,
		GenerationID:        generationID,
		CollectorInstanceID: collectorInstanceID,
		FencingToken:        target.FencingToken,
		ObservedAt:          observedAt,
		SourceURI:           target.SourceURI,
	})
}

func (s *Source) scopeAndGeneration(target TargetConfig, observedAt time.Time, references []string, generationID string) (string, Generation, error) {
	identity, err := oci.NormalizeRepositoryIdentity(target.identity())
	if err != nil {
		return "", Generation{}, err
	}
	generationID = strings.TrimSpace(generationID)
	if generationID == "" {
		fingerprint := stableid.StableID("OCIRegistryGeneration", map[string]any{
			"provider":   string(identity.Provider),
			"registry":   identity.Registry,
			"repository": identity.Repository,
			"refs":       references,
			"observed":   observedAt.UTC().Format(time.RFC3339Nano),
		})
		generationID = "oci-registry:" + fingerprint
	}
	return identity.ScopeID, Generation{
		ID:         generationID,
		ObservedAt: observedAt,
		// Attested deletions bypass the successor-emptiness backstop (an emptied scope still emits its repository fact).
		TrustedDeletion: target.AllowEmptySuccessor || s.Config.AllowEmptySuccessor,
	}, nil
}

func (s *Source) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
