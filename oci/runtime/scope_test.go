// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
)

func TestTargetConfigScopeID(t *testing.T) {
	t.Parallel()

	got, err := TargetConfig{
		Provider:   oci.ProviderDockerHub,
		Registry:   "docker.io",
		Repository: "library/nginx",
	}.ScopeID()
	if err != nil {
		t.Fatalf("ScopeID() error = %v", err)
	}
	if want := "oci-registry://docker.io/library/nginx"; got != want {
		t.Fatalf("ScopeID() = %q, want %q", got, want)
	}
}

func TestTargetConfigScopeIDRejectsEmptyRepository(t *testing.T) {
	t.Parallel()

	got, err := TargetConfig{
		Provider: oci.ProviderDockerHub,
		Registry: "docker.io",
	}.ScopeID()
	if err == nil {
		t.Fatalf("ScopeID() = %q, want an identity error", got)
	}
	if !strings.Contains(err.Error(), "oci repository name must not be blank") {
		t.Fatalf("ScopeID() error = %q, want the repository identity failure", err)
	}
	if got != "" {
		t.Fatalf("ScopeID() = %q on error, want empty", got)
	}
}
