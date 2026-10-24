// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

const (
	// ComponentID is the manifest identity of this collector. Keep it in sync
	// with metadata.id in manifest.yaml.
	ComponentID = "dev.eshu.collector.oci-registry"
	// CollectorKind is the durable collector family name for OCI registry facts.
	CollectorKind = oci.CollectorKind
	// SourceSystem is the source system name facts carry when the claim does
	// not name one. Hosted claims carry the activation's source system.
	SourceSystem = oci.SourceSystem
	// MetricsPrefix is the component-owned metric prefix the manifest declares.
	// Nothing in this repository emits a metric under it yet; see the README
	// section "Operator signals".
	MetricsPrefix = "eshu_dp_oci_registry_collector_"
	// SchemaVersion is the schema version of every emitted oci_registry fact.
	SchemaVersion = oci.SchemaVersion
)

// Fact kind aliases. They are core-owned: the host requires a producer grant
// for each before accepting them from this component.
const (
	// FactKindRepository identifies one OCI registry repository configured
	// for observation.
	FactKindRepository = oci.FactKindRepository
	// FactKindTagObservation identifies one mutable tag-to-digest observation.
	FactKindTagObservation = oci.FactKindTagObservation
	// FactKindManifest identifies one digest-addressed image manifest.
	FactKindManifest = oci.FactKindManifest
	// FactKindIndex identifies one digest-addressed image index.
	FactKindIndex = oci.FactKindIndex
	// FactKindDescriptor identifies one reusable OCI descriptor.
	FactKindDescriptor = oci.FactKindDescriptor
	// FactKindReferrer identifies one descriptor reported as a referrer.
	FactKindReferrer = oci.FactKindReferrer
	// FactKindWarning identifies one non-fatal OCI registry warning.
	FactKindWarning = oci.FactKindWarning
)

// FactKinds returns every fact kind this collector may emit, in the order the
// manifest declares them.
func FactKinds() []string {
	return oci.FactKinds()
}

// Contract returns the SDK fact families this collector may emit.
func Contract() sdk.Contract {
	kinds := FactKinds()
	facts := make([]sdk.FactDeclaration, 0, len(kinds))
	for _, kind := range kinds {
		facts = append(facts, sdk.FactDeclaration{
			Kind:             kind,
			SchemaVersions:   []string{SchemaVersion},
			SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
		})
	}
	return sdk.Contract{ProtocolVersion: sdk.ProtocolVersionV1Alpha1, Facts: facts}
}
