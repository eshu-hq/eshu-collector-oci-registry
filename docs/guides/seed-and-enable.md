# Seed and enable

From a published image to a live activation, in order.

## Image and seeding

The published image is `ghcr.io/eshu-hq/eshu-collector-oci-registry`.
It holds one static binary with no shell, and the binary installs
itself into a component folder, which is how a startup (init)
container sets it up:

```bash
collector seed --dest /component [--image-ref ghcr.io/eshu-hq/eshu-collector-oci-registry@sha256:<digest>]
```

That writes `/component/collector` (mode 0755) and
`/component/manifest.yaml`. `--image-ref` (or `COLLECTOR_IMAGE_REF`;
the flag wins) stamps the exact deployed image reference into the
manifest, because an image can't contain its own digest. The manifest
in this repo keeps an all-zero placeholder digest on purpose.
`scripts/pin-digest.sh <repo:tag>` only prints the `repo@sha256:...`
reference of a pushed image; it edits nothing.

## Then record grants and enable

The host refuses this component's facts without a producer grant for
each kind. The seven requests are in
[grant.example.json](../../grant.example.json), tied to `dev.eshu.collector.oci-registry`
and its manifest version. Record them before you enable (see
[concepts](../concepts/README.md) for why grants exist), enable one
activation per scope from your copy of
[config.example.yaml](../../config.example.yaml), then let Eshu run a
first claim and check it with [operate](operate.md).

If you're replacing Eshu's built-in collector, do the identity checks
in the [configuration reference](../reference/configuration.md) first.
Getting those wrong re-keys stored data, and that's much easier to
prevent than to undo.
