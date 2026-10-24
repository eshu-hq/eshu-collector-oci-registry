// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"encoding/json"
	"time"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

const maxOCIImageConfigBlobBytes int64 = 1 << 20

func (s *Source) configProvenanceLabels(
	ctx context.Context,
	client RegistryClient,
	target TargetConfig,
	collectorInstanceID string,
	generationID string,
	observedAt time.Time,
	config oci.Descriptor,
) (map[string]string, []sdk.Fact, error) {
	if config.Digest == "" {
		return nil, nil, nil
	}
	if config.SizeBytes > maxOCIImageConfigBlobBytes {
		warning, err := s.warningFact(
			target,
			collectorInstanceID,
			generationID,
			observedAt,
			oci.WarningConfigBlobOversized,
			"image config blob exceeds bounded provenance label read limit",
			config.Digest,
		)
		return nil, []sdk.Fact{warning}, err
	}

	var blobLabels map[string]string
	var blobSize int64
	blob, err := client.GetBlob(ctx, target.Repository, config.Digest)
	if err == nil {
		blobSize = int64(len(blob.Body))
		if blobSize <= maxOCIImageConfigBlobBytes {
			blobLabels = imageConfigLabels(blob.Body)
		}
	}
	if err != nil {
		warning, warningErr := s.warningFact(
			target,
			collectorInstanceID,
			generationID,
			observedAt,
			oci.WarningConfigBlobUnavailable,
			"image config blob unavailable for provenance label read",
			config.Digest,
		)
		return nil, []sdk.Fact{warning}, warningErr
	}
	if blobSize > maxOCIImageConfigBlobBytes {
		warning, warningErr := s.warningFact(
			target,
			collectorInstanceID,
			generationID,
			observedAt,
			oci.WarningConfigBlobOversized,
			"image config blob exceeds bounded provenance label read limit",
			config.Digest,
		)
		return nil, []sdk.Fact{warning}, warningErr
	}
	return blobLabels, nil, nil
}

func imageConfigLabels(body []byte) map[string]string {
	var decoded struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil
	}
	if len(decoded.Config.Labels) == 0 {
		return nil
	}
	return decoded.Config.Labels
}
