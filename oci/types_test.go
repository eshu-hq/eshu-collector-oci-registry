// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oci

import (
	"reflect"
	"strings"
	"testing"
)

func TestFactKinds(t *testing.T) {
	t.Parallel()

	want := []string{
		FactKindRepository,
		FactKindTagObservation,
		FactKindManifest,
		FactKindIndex,
		FactKindDescriptor,
		FactKindReferrer,
		FactKindWarning,
	}
	got := FactKinds()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FactKinds() = %v, want %v", got, want)
	}
	seen := make(map[string]struct{}, len(got))
	for _, kind := range got {
		if !strings.HasPrefix(kind, "oci_registry.") {
			t.Fatalf("FactKinds() carries %q, want the oci_registry family", kind)
		}
		if _, dup := seen[kind]; dup {
			t.Fatalf("FactKinds() repeats %q", kind)
		}
		seen[kind] = struct{}{}
	}
}
