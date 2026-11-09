# Facts reference: OCI registry collector

This table mirrors [manifest.yaml](../../manifest.yaml). The manifest
is the source of truth, so if you change one you need to update the
other in the same change.

| Fact kind | Schema | Confidence | One fact is |
| --- | --- | --- | --- |
| `oci_registry.repository` | `1.0.0` | `reported` | the repository itself |
| `oci_registry.image_tag_observation` | `1.0.0` | `reported` | one tag seen in the repository |
| `oci_registry.image_manifest` | `1.0.0` | `reported` | one image manifest |
| `oci_registry.image_index` | `1.0.0` | `reported` | one multi-platform image index |
| `oci_registry.image_descriptor` | `1.0.0` | `reported` | one referenced blob: layer, config, or platform entry |
| `oci_registry.image_referrer` | `1.0.0` | `reported` | one file attached to an image |
| `oci_registry.warning` | `1.0.0` | `reported` | something the registry hid or the read had to skip |

The component is `dev.eshu.collector.oci-registry`, version `0.1.0`,
collector kind `oci_registry`. It declares Eshu core `>=0.0.5 <0.2.0`
and speaks `collector-sdk/v1alpha1` over the process adapter.

Every kind above belongs to Eshu core, so the host won't accept it
without a producer grant. The seven requests live in
[grant.example.json](../../grant.example.json); see
[concepts](../concepts/README.md) for how grants and identity work,
and [seed and enable](../guides/seed-and-enable.md) for recording
them.
