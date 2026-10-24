// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
)

// ScanClaim scans the configured target whose scope ID matches the claimed
// scope. The caller supplies the claim's generation ID so retries stay
// idempotent, and the claim's fencing token so the scan carries the
// host-enforced boundary the host keys grant checks on.
func (s *Source) ScanClaim(ctx context.Context, scopeID, generationID string, fencingToken int64) (ScanResult, bool, error) {
	config, err := s.Config.validated()
	if err != nil {
		return ScanResult{}, false, err
	}
	if s.ClientFactory == nil {
		return ScanResult{}, false, fmt.Errorf("OCI registry client factory is required")
	}
	var matched *TargetConfig
	for _, target := range config.Targets {
		scope, err := targetScopeID(target)
		if err != nil {
			return ScanResult{}, false, err
		}
		if scope != scopeID {
			continue
		}
		if matched != nil {
			return ScanResult{}, false, fmt.Errorf("OCI registry scan found multiple configured targets for scope_id %q", scopeID)
		}
		targetCopy := target
		matched = &targetCopy
	}
	if matched == nil {
		return ScanResult{}, false, nil
	}
	matched.FencingToken = fencingToken
	collected, err := s.scanTarget(ctx, config, *matched, generationID)
	return collected, true, err
}

func targetScopeID(target TargetConfig) (string, error) {
	identity, err := oci.NormalizeRepositoryIdentity(target.identity())
	if err != nil {
		return "", err
	}
	return identity.ScopeID, nil
}
