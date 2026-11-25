# Roadmap

The design behind these milestones is in
[docs/design/0001-architecture.md](docs/design/0001-architecture.md).

## v0.1: walking skeleton (current)

- `gapdrill run`, `validate`, `cleanup` and `version`.
- Isolation: bundle registry with no upstream fallback, NXDOMAIN DNS, egress
  rules in every node, verified before any operation runs.
- Operations: Helm install, Helm test, run a CronJob, exec.
- Reports in Markdown and JSON, with findings attributed to operations and
  owners.
- A demo that fails on two realistic offline bugs and passes once they're
  fixed.

## v0.2: easy to adopt in CI

- GitHub Action.
- Release binaries for Linux and macOS, signed, with SBOMs.
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
