// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// errLimitsUnknownField rejects a limits key outside the four documented
// budget bounds. It carries no key or value, so a misplaced credential key
// can never be echoed.
var errLimitsUnknownField = errors.New("unknown field")

// ResourceUse reports and enforces the bounded cost of one claim. Every
// non-positive field selects its DefaultResourceUse value, so a partially
// customized value keeps the caller's settings and only the gaps fall back to
// defaults.
type ResourceUse struct {
	// MaxRecordsPerClaim bounds one claim's fact emission.
	MaxRecordsPerClaim int `json:"max_records_per_claim"`
	// MaxPayloadBytes bounds one emitted fact payload.
	MaxPayloadBytes int `json:"max_payload_bytes"`
	// MaxResultBytes bounds the encoded result written to stdout. It must
	// stay below the host's process stdout limit (4 MiB unless the activation
	// sets process.stdout_limit_bytes): the collector fails terminal with a
	// named class instead of letting the host drop an oversized result.
	MaxResultBytes int `json:"max_result_bytes"`
	// ClaimTimeoutSeconds bounds one claim's wall time.
	ClaimTimeoutSeconds int `json:"claim_timeout_seconds"`
}

const (
	maxRecordsCeiling = 1_000_000
	maxPayloadCeiling = 16 << 20
	maxResultCeiling  = 256 << 20
	maxTimeoutCeiling = 3600
)

// UnmarshalJSON decodes the four enforcement bounds from either the
// snake_case keys of the activation file (max_records_per_claim, ...) or the
// camelCase keys the component-extension host forwards verbatim in the SDK
// request config (maxRecordsPerClaim, ...). A key outside those eight
// spellings fails closed without naming the key, because a misplaced
// credential key must never be echoed. When both spellings of one bound are
// present the snake_case activation spelling wins; it matches
// config.example.yaml and is the operator's deliberate choice.
func (r *ResourceUse) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out ResourceUse
	take := func(snake, camel string, dest *int) error {
		snakeRaw, hasSnake := raw[snake]
		camelRaw, hasCamel := raw[camel]
		switch {
		case hasSnake && hasCamel:
			// Both spellings present: decode both so each keeps its
			// wrong-type rejection, then the activation spelling wins.
			if err := json.Unmarshal(camelRaw, dest); err != nil {
				return fmt.Errorf("field %s has the wrong type", snake)
			}
			if err := json.Unmarshal(snakeRaw, dest); err != nil {
				return fmt.Errorf("field %s has the wrong type", snake)
			}
		case hasSnake:
			if err := json.Unmarshal(snakeRaw, dest); err != nil {
				return fmt.Errorf("field %s has the wrong type", snake)
			}
		case hasCamel:
			if err := json.Unmarshal(camelRaw, dest); err != nil {
				return fmt.Errorf("field %s has the wrong type", snake)
			}
		}
		return nil
	}
	if err := take("max_records_per_claim", "maxRecordsPerClaim", &out.MaxRecordsPerClaim); err != nil {
		return err
	}
	if err := take("max_payload_bytes", "maxPayloadBytes", &out.MaxPayloadBytes); err != nil {
		return err
	}
	if err := take("max_result_bytes", "maxResultBytes", &out.MaxResultBytes); err != nil {
		return err
	}
	if err := take("claim_timeout_seconds", "claimTimeoutSeconds", &out.ClaimTimeoutSeconds); err != nil {
		return err
	}
	for key := range raw {
		switch key {
		case "max_records_per_claim", "maxRecordsPerClaim",
			"max_payload_bytes", "maxPayloadBytes",
			"max_result_bytes", "maxResultBytes",
			"claim_timeout_seconds", "claimTimeoutSeconds":
		default:
			return errLimitsUnknownField
		}
	}
	*r = out
	return nil
}

// DefaultResourceUse is the starting bound. The result bound sits under the
// host's default 4 MiB stdout limit; a site with larger evidence raises both
// limits.max_result_bytes and the activation's process.stdout_limit_bytes.
func DefaultResourceUse() ResourceUse {
	return ResourceUse{
		MaxRecordsPerClaim:  50_000,
		MaxPayloadBytes:     1 << 20,
		MaxResultBytes:      3 << 20,
		ClaimTimeoutSeconds: 300,
	}
}

// clamp bounds every positive field to its ceiling, so a large
// activation/host value cannot size per-claim memory/CPU without limit.
// Non-positive fields are left for withDefaults/overlay to fill.
func (r ResourceUse) clamp() ResourceUse {
	if r.MaxRecordsPerClaim > maxRecordsCeiling {
		r.MaxRecordsPerClaim = maxRecordsCeiling
	}
	if r.MaxPayloadBytes > maxPayloadCeiling {
		r.MaxPayloadBytes = maxPayloadCeiling
	}
	if r.MaxResultBytes > maxResultCeiling {
		r.MaxResultBytes = maxResultCeiling
	}
	if r.ClaimTimeoutSeconds > maxTimeoutCeiling {
		r.ClaimTimeoutSeconds = maxTimeoutCeiling
	}
	return r
}

// overlay returns r with every non-positive field taken from base, so a
// partially customized activation keeps the handler's settings and only the
// gaps fall back. The merged result is clamped to the ceilings.
func (r ResourceUse) overlay(base ResourceUse) ResourceUse {
	if r.MaxRecordsPerClaim <= 0 {
		r.MaxRecordsPerClaim = base.MaxRecordsPerClaim
	}
	if r.MaxPayloadBytes <= 0 {
		r.MaxPayloadBytes = base.MaxPayloadBytes
	}
	if r.MaxResultBytes <= 0 {
		r.MaxResultBytes = base.MaxResultBytes
	}
	if r.ClaimTimeoutSeconds <= 0 {
		r.ClaimTimeoutSeconds = base.ClaimTimeoutSeconds
	}
	return r.clamp()
}

// withDefaults fills every non-positive field from DefaultResourceUse, then
// clamps every field to its ceiling.
func (r ResourceUse) withDefaults() ResourceUse {
	defaults := DefaultResourceUse()
	if r.MaxRecordsPerClaim <= 0 {
		r.MaxRecordsPerClaim = defaults.MaxRecordsPerClaim
	}
	if r.MaxPayloadBytes <= 0 {
		r.MaxPayloadBytes = defaults.MaxPayloadBytes
	}
	if r.MaxResultBytes <= 0 {
		r.MaxResultBytes = defaults.MaxResultBytes
	}
	if r.ClaimTimeoutSeconds <= 0 {
		r.ClaimTimeoutSeconds = defaults.ClaimTimeoutSeconds
	}
	return r.clamp()
}
