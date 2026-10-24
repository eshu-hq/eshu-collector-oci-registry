#!/usr/bin/env bash
# Records the seven core producer grants this collector needs, one per
# oci_registry.* kind, in a component home. The grant scope is the manifest's
# collector kind ("oci_registry"), not a repository: core binds a grant to the
# kinds the manifest declares. Core issues grants; run this only against a
# home you are authorized to grant in, after the grant request is approved.
#
#   COMPONENT_HOME=/path/to/home ESHU=eshu scripts/record-grants.sh [version] [lifetime]
set -euo pipefail
: "${COMPONENT_HOME:?COMPONENT_HOME is not set: no grants recorded}"
eshu="${ESHU:-eshu}"
version="${1:-0.1.0}"
lifetime="${2:-2160h}"
for kind in \
  oci_registry.repository oci_registry.image_tag_observation \
  oci_registry.image_manifest oci_registry.image_index \
  oci_registry.image_descriptor oci_registry.image_referrer \
  oci_registry.warning; do
  "$eshu" component grant dev.eshu.collector.oci-registry \
    --kind "$kind" --schema-version 1.0.0 --scope oci_registry \
    --version "$version" --expires-in "$lifetime" \
    --component-home "$COMPONENT_HOME"
done
