# eshu-collector-oci-registry

Status: pre-release. Please don't rely on it in production yet.

In a nutshell: this collector reads one container-image repository
from an OCI registry and hands Eshu a checked summary of its tags and
image files. Eshu is the core system, and a collector is a small
helper that reads one outside system and turns what it finds into
facts (small, typed records). OCI is the open standard most container
registries speak, so Docker Hub, GHCR, ECR, and friends all work the
same basic way here. This collector only reads. It never pushes,
deletes, or changes an image.

```mermaid
flowchart LR
  A["OCI registry, one repository"] --> B["This collector"] --> C["Eshu host process"] --> D["Eshu core"]
```

Text version: one OCI registry repository, then this collector, then
the Eshu host process (it checks and stores the result), then Eshu
core. The collector never talks to Eshu's database directly.

## Good fit, bad fit

It's a good fit when Eshu needs to know which tags, images, and
attached files exist in one registry repository, refreshed on a
schedule Eshu controls.

It's a bad fit for publishing or deleting images, scanning image
contents layer by layer, or browsing a registry in general. You get
registry metadata for the repositories you set up, and that's the
whole job.

## How Eshu runs it

Eshu's component host starts the program for one claim (one job) at a time.
It sends one bounded `collector-sdk/v1alpha1` request on stdin and
reads one result from stdout: `collector --sdk-stdio`.
The collector gets no database, graph, queue, or lease handle.
The host checks the result and owns claims and commits.

## Documentation

| Page | What's in it |
| --- | --- |
| [Documentation](docs/README.md) | Start here: the whole docs tree in one index |
| [Getting started](docs/getting-started/README.md) | The fastest honest path to a first successful claim |
| [Concepts](docs/concepts/README.md) | What it does, the facts story, identity and grants, and what it never does |
| [Configuration](docs/reference/configuration.md) | Every setting, credentials, limits, and identity on a switch |
| [Facts](docs/reference/facts.md) | The fact kinds, schemas, and confidence, mirroring the manifest |
| [Guides](docs/guides/README.md) | Seed and enable, operate, troubleshoot, release |
| [Diagrams](docs/diagrams/README.md) | Every diagram, each with a text version |
| [Example config](config.example.yaml) | A full activation for one repository, ready to copy |
| [Manifest](manifest.yaml) | Component identity, fact list, and the core version range |
| [Grant requests](grant.example.json) | The seven producer grants this component asks core for |
| [Contributing](CONTRIBUTING.md) | The lane contract and the local check |

## Related repos

- [Eshu core](https://github.com/eshu-hq/eshu) is the host that runs
  this collector.
- [eshu-sdk](https://github.com/eshu-hq/eshu-sdk) holds the SDK modules
  pinned in `go.mod` for this repo.
- [eshu-deploy](https://github.com/eshu-hq/eshu-deploy) is related
  reading for deployment examples. It isn't a build dependency, and
  nothing in this tree references it outside this list.

Exact pins live in [configuration](docs/reference/configuration.md). Short
version: `github.com/eshu-hq/eshu-sdk/collector v0.2.0-rc.1` and
`github.com/eshu-hq/eshu-sdk/factschema v0.1.0-rc.6`, on Go `1.26.10`.

## Development

Run `make ci` before you push; it's lint, tests, and the race suite,
same as CI.

## Contributing, security, and license

[CONTRIBUTING.md](CONTRIBUTING.md) has the details, including a plea
not to weaken the parity, hostile-input, or boundary tests to land a
change. There's no separate security policy file yet, so please
report a suspected vulnerability or credential leak to the maintainers
privately instead of opening a public issue. MIT, in
[LICENSE](LICENSE).
