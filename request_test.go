// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestParseRequestRoundTrip proves the host stdin document decodes: a
// marshaled valid request parses back to the same claim identity and
// config, so the binary's entry point accepts what the host sends.
func TestParseRequestRoundTrip(t *testing.T) {
	t.Parallel()

	config := handlerTargetConfig()
	raw, err := json.Marshal(Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          config,
	})
	if err != nil {
		t.Fatalf("json.Marshal(Request) error = %v", err)
	}
	got, err := ParseRequest(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}
	if got.Claim.Scope.ID != "oci-registry://registry-1.docker.io/library/busybox" {
		t.Fatalf("Scope.ID = %q, want the claimed repository", got.Claim.Scope.ID)
	}
	if got.Config["provider"] != "dockerhub" {
		t.Fatalf("Config provider = %v, want dockerhub", got.Config["provider"])
	}
}

// TestParseRequestRejectsMalformedInput proves the stdin boundary fails
// closed without quoting the input: oversize documents, unknown fields,
// trailing JSON, wrong protocol versions, and claims missing identity are
// all rejected.
func TestParseRequestRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	valid, err := json.Marshal(Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          handlerTargetConfig(),
	})
	if err != nil {
		t.Fatalf("json.Marshal(Request) error = %v", err)
	}
	cases := map[string]string{
		"oversize":      strings.Repeat("x", MaxRequestBytes+1),
		"unknown field": `{"protocol_version":"` + sdk.ProtocolVersionV1Alpha1 + `","claim":{},"contract":{},"smuggled":1}`,
		"trailing JSON": string(valid) + " {}",
		"wrong version": `{"protocol_version":"v9","claim":{},"contract":{}}`,
		"empty":         "",
		"missing claim": `{"protocol_version":"` + sdk.ProtocolVersionV1Alpha1 + `","claim":{"component_id":""},"contract":{}}`,
		"wrong kind":    `{"protocol_version":"` + sdk.ProtocolVersionV1Alpha1 + `","claim":{"component_id":"c","instance_id":"i","collector_kind":"nope","source_system":"s","scope":{"id":"x","kind":"y"},"generation_id":"g"},"contract":{}}`,
		"not JSON":      "{not json",
	}
	for name, input := range cases {
		if _, err := ParseRequest(strings.NewReader(input)); err == nil {
			t.Errorf("%s: ParseRequest() error = nil, want rejection", name)
		}
	}
}
