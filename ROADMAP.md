# Roadmap

The design behind these milestones is in
[docs/design/0001-architecture.md](docs/design/0001-architecture.md). Where the
project is heading, and why, is in [docs/vision.md](docs/vision.md). Ideas
that aren't scheduled yet are in [docs/ideas.md](docs/ideas.md).

## v0.1: walking skeleton (current)

- `gapdrill run`, `validate`, `cleanup` and `version`.
- Isolation: bundle registry with no upstream fallback, NXDOMAIN DNS, egress
  rules in every node, verified before any operation runs.
- Operations: Helm install, Helm test, run one CronJob or all of them, exec.
- Zero-config drills: `gapdrill run --chart … --images …` and `gapdrill init`
  ([design](docs/design/0002-zero-config-drills.md)).
- Reports in Markdown and JSON, with findings attributed to operations and
  owners.
- Reports list the bundle images nothing pulled, so bundles can be trimmed.
- A demo that fails on two realistic offline bugs and passes once they're
  fixed.
- Release binaries for Linux and macOS, signed, with SBOMs.
- A GitHub Action that runs a drill and publishes the report.

## v0.2: easy to adopt in CI

- A probe pod, to verify pod egress directly as well as by rule presence.
- Blocked connections attributed to the pods that made them.

## v0.3: real bundles and operators

- Bundle inputs: OCI layout or tarball, Zarf package, Hauler store.
- Operator workflows: apply manifests and wait for conditions.

## v0.4: faster feedback

- Static pre-check comparing rendered images with the bundle, as a fast first
  signal before the cluster run.
- Multi-architecture runs.

## v1: stable

- Stable drill and report formats.
- At least two projects outside the maintainers' organizations using gapdrill
  in their release pipelines, and at least one documented packaging mistake
  it caught before users received a release.
