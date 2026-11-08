# Getting started

Status: pre-release. Please don't rely on this in production yet.

The honest first thing to know: you don't run this collector as a
daemon. Eshu's host starts it for one claim (one job) at a time, sends
one bounded request on stdin, and reads one result from stdout. So
"getting started" really means getting one activation enabled and one
claim to succeed.

1. Check your core version fits the range in the [manifest](../../manifest.yaml)
   (`>=0.0.5 <0.2.0`).
2. Copy [config.example.yaml](../../config.example.yaml). Set `provider`,
   `registry`, `repository`, a sensible `tag_limit`, and the required
   `source_uri`. Public repositories can use `auth_mode: anonymous`;
   private logins ride in the `*_env` variables, and ECR logins are
   minted per claim from the worker's AWS credentials. See the
   [configuration reference](../reference/configuration.md).
3. Record the seven producer grants from
   [grant.example.json](../../grant.example.json) with
   `scripts/record-grants.sh`.
4. Seed the image and enable the activation (see
   [seed and enable](../guides/seed-and-enable.md)). If you're replacing
   the built-in collector, keep `host.scope.id` as
   `oci-registry://<registry>/<repository>`, kind
   `container_registry_repository`, and source system `oci_registry`.
5. Let Eshu run a first claim. `complete`, or `partial` with
   `oci_registry.warning` facts, means it's working.

Where to go next: [concepts](../concepts/README.md) for the mental
model, the [configuration reference](../reference/configuration.md)
for every setting, [facts](../reference/facts.md) for what comes out,
and [troubleshoot](../guides/troubleshoot.md) when the first claim
doesn't land.
