# Release

## Build, test, and release

```bash
go build ./...
go test ./...
go test -race ./oci/... .
make ci   # lint + test + race; the full local check CI runs
```

See `CONTRIBUTING.md` for the lane contract
(`.github/workflows/ci.yml`, reported as `go-ci / ci-complete`). For
releases, `metadata.version` in `manifest.yaml` and the git tag must
match (`v0.1.0` for `0.1.0`), and tests enforce that in both
directions, including the placeholder-digest rule. The release
workflow (`.github/workflows/release.yml`) publishes only when a
version tag points at the protected `main` head, so merge first and
then tag that head. Every push to `main` also publishes the moving
`:main`, `:latest`, and `:sha-<long-sha>` image tags, with the same
provenance, cosign signing, SBOM, and attestation checks as a version
release. Pin real deployments to digests, not moving tags.
