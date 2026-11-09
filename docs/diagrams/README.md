# Diagrams

Every diagram for this collector lives on this page, so there's one
place to keep them honest. Other pages link here instead of copying
the picture.

## System context

How a source system, this collector, the Eshu host, and Eshu core
fit together. The collector only talks to its source and to the
host over stdin/stdout.

```mermaid
flowchart LR
  A["OCI registry, one repository"] --> B["This collector"] --> C["Eshu host process"] --> D["Eshu core"]
```

Text version: OCI registry repository, then this collector, then the
Eshu host process (checks and stores the result), then Eshu core.

## Claim lifecycle

What happens to one claim, from the host sending it on stdin to the
host committing the result or recording a failure class. The
collector checks config and credentials before any provider call.

```mermaid
sequenceDiagram
  participant H as Eshu host
  participant C as This collector
  participant R as OCI registry
  H->>C: one claim on stdin
  C->>C: match the target to the claim scope, resolve the login
  C->>R: read tags, manifests, indexes, descriptors, referrers
  R-->>C: registry responses
  C->>C: filter labels, build facts, check budgets
  C-->>H: one result on stdout
  H->>H: validate and commit, or record the failure class
```

Text version: the host sends a claim, the collector matches it to a
configured target, reads the repository within its limits, filters and
builds facts, and returns one result for the host to validate and
commit.
