// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func validTargetConfig() map[string]any {
	return map[string]any{
		"provider": "dockerhub", "registry": "registry-1.docker.io",
		"repository": "library/busybox", "tag_limit": 10,
		"source_uri": "https://registry-1.docker.io/v2/library/busybox",
	}
}

// TestResolveTargetRejectsUnknownFieldsClosed proves the config surface fails
// closed: an unknown key is rejected without naming it, because a pasted
// credential must never be echoed back.
func TestResolveTargetRejectsUnknownFieldsClosed(t *testing.T) {
	t.Parallel()

	config := validTargetConfig()
	config["token"] = "SECRET"
	_, _, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
	if err == nil {
		t.Fatal("resolveTarget() error = nil, want unknown-field rejection")
	}
	if strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("rejection echoes the key or value: %v", err)
	}
}

// TestResolveTargetRejectsBadCredentialReference proves a credential pasted
// into an env reference cannot be echoed: the rejection names the config key
// only.
func TestResolveTargetRejectsBadCredentialReference(t *testing.T) {
	t.Parallel()

	config := validTargetConfig()
	config["password_env"] = "SECRET-123"
	_, _, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
	if err == nil {
		t.Fatal("resolveTarget() error = nil, want env-name rejection")
	}
	if !strings.Contains(err.Error(), "password_env") {
		t.Fatalf("rejection = %v, want it to name password_env", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("rejection echoes the credential: %v", err)
	}
}

// TestResolveTargetRequiresSourceURI proves a blank source URI fails fast:
// the host validator requires one on every fact.
func TestResolveTargetRequiresSourceURI(t *testing.T) {
	t.Parallel()

	config := validTargetConfig()
	delete(config, "source_uri")
	if _, _, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" }); err == nil {
		t.Fatal("resolveTarget() error = nil, want source_uri rejection")
	}
}

// TestResolveTargetReadsLimits proves the documented limits knob is wired:
// a snake_case limits object decodes into the enforcement bounds the handler
// uses, instead of failing closed as an unknown field.
func TestResolveTargetReadsLimits(t *testing.T) {
	t.Parallel()

	config := validTargetConfig()
	config["limits"] = map[string]any{
		"max_records_per_claim": 100,
		"max_payload_bytes":     2048,
		"max_result_bytes":      4096,
		"claim_timeout_seconds": 60,
	}
	_, limits, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveTarget() error = %v", err)
	}
	want := ResourceUse{
		MaxRecordsPerClaim:  100,
		MaxPayloadBytes:     2048,
		MaxResultBytes:      4096,
		ClaimTimeoutSeconds: 60,
	}
	if limits != want {
		t.Fatalf("limits = %+v, want %+v", limits, want)
	}
}

// TestExampleConfigResolvesThroughDecoder proves the shipped activation
// example is accepted verbatim: its config map decodes and carries the
// documented limits into enforcement.
func TestExampleConfigResolvesThroughDecoder(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("config.example.yaml")
	if err != nil {
		t.Fatalf("read example activation: %v", err)
	}
	var activation struct {
		Config map[string]any `yaml:"config"`
	}
	if err := yaml.Unmarshal(raw, &activation); err != nil {
		t.Fatalf("unmarshal example activation: %v", err)
	}
	encoded, err := json.Marshal(activation.Config)
	if err != nil {
		t.Fatalf("marshal example config: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(encoded, &config); err != nil {
		t.Fatalf("decode example config: %v", err)
	}
	_, limits, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveTarget(example) error = %v", err)
	}
	if limits.MaxResultBytes != 50331648 {
		t.Fatalf("limits.MaxResultBytes = %d, want 50331648", limits.MaxResultBytes)
	}
	if limits.MaxRecordsPerClaim != 50000 || limits.MaxPayloadBytes != 1048576 || limits.ClaimTimeoutSeconds != 300 {
		t.Fatalf("limits = %+v, want the example values", limits)
	}
}

// TestResolveTargetMatchesMultiTargetScope proves a multi-target activation
// resolves the claim to its scope and rejects a scope with no target.
func TestResolveTargetMatchesMultiTargetScope(t *testing.T) {
	t.Parallel()

	first := validTargetConfig()
	second := map[string]any{
		"provider": "ghcr", "registry": "ghcr.io",
		"repository": "eshu-hq/parity-demo", "tag_limit": 1,
		"source_uri": "https://ghcr.io/v2/eshu-hq/parity-demo",
	}
	config := map[string]any{"targets": []any{first, second}}
	target, _, err := resolveTarget(config, "oci-registry://ghcr.io/eshu-hq/parity-demo", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveTarget() error = %v", err)
	}
	if scope, err := target.ScopeID(); err != nil || scope != "oci-registry://ghcr.io/eshu-hq/parity-demo" {
		t.Fatalf("ScopeID() = %q, %v", scope, err)
	}
	if _, _, err := resolveTarget(config, "oci-registry://example.invalid/unknown", func(string) string { return "" }); err == nil {
		t.Fatal("resolveTarget(unknown) error = nil, want rejection")
	}
}
