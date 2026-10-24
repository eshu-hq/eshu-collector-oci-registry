// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"
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
	_, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
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
	_, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" })
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
	if _, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", func(string) string { return "" }); err == nil {
		t.Fatal("resolveTarget() error = nil, want source_uri rejection")
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
	target, err := resolveTarget(config, "oci-registry://ghcr.io/eshu-hq/parity-demo", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveTarget() error = %v", err)
	}
	if scope, err := target.ScopeID(); err != nil || scope != "oci-registry://ghcr.io/eshu-hq/parity-demo" {
		t.Fatalf("ScopeID() = %q, %v", scope, err)
	}
	if _, err := resolveTarget(config, "oci-registry://example.invalid/unknown", func(string) string { return "" }); err == nil {
		t.Fatal("resolveTarget(unknown) error = nil, want rejection")
	}
}
