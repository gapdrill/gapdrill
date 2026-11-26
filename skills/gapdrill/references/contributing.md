# Working on gapdrill

Read this when you're changing gapdrill's own code, docs, Action or release
setup in `github.com/gapdrill/gapdrill`. CONTRIBUTING.md and GOVERNANCE.md in
the repository are the authority; this is the working summary.

## Layout

| Path | What's there |
|---|---|
| `cmd/gapdrill` | `main` |
| `internal/cli` | Cobra commands: `run`, `init`, `compare`, `validate`, `cleanup`, `version` |
| `internal/spec` | The drill format: types and defaults (`spec.go`), validation (`validate.go`), strict loading and image lists (`load.go`), and zero-config drills and `init` (`auto.go`) |
| `internal/runner` | One drill end to end: prepare, isolate, verify, operations, collect, report and teardown (`runner.go`, `phases.go`), plus `progress.go` and `cleanup.go`. Exit codes live here. |
| `internal/ops` | The operations: Helm install and test, `runCronJob` with the 20-second pull watch, `exec` (`ops.go`). `runCronJobs` and `helmTest.ifPresent` are expanded at run time (`expand.go`). |
| `internal/cluster` | kind, used as a Go library: one node, with a kubeconfig in a temporary directory |
| `internal/registry` | The bundle registry: copying images, and parsing its access log (`accesslog.go`) |
| `internal/mirror` | Image references, and the containerd `hosts.toml` files for strict and learn runs |
| `internal/lockdown` | The iptables and ip6tables egress rules inside the node, and the probe |
| `internal/dnsmon` | The offline CoreDNS Corefile, and parsing its query log |
| `internal/observe` | Watches pods; resolves owners; keeps an IP-to-pod history |
| `internal/evidence` | Evidence types |
| `internal/learn` | Learn mode: diffs `crictl images` before and after the operations |
| `internal/report` | Evidence into findings (`build.go`); report types (`report.go`); Markdown; `compare.go` (ids); `fix.go`; `egress.go` (CycloneDX); `learned.go` |
| `action.yml`, `action/*.sh` | The composite GitHub Action: `resolve.sh`, `install.sh`, `run.sh` |
| `test/e2e` | End-to-end tests (build tag `e2e`) that drill `examples/demo` on real kind clusters |
| `docs/design/` | Design docs `0001`... |
| `docs/ideas.md`, `ROADMAP.md`, `docs/vision.md` | Proposals, the plan, and the direction |

## Commands

You need Go 1.26 or later. The e2e tests also need Docker and Helm.

```sh
make build                      # bin/gapdrill
make test                       # go test -race -count=1 ./... (no Docker needed)
make lint                       # golangci-lint v2 (gofmt, goimports, errorlint, misspell, ...)
shellcheck action/*.sh          # CI lints the Action's scripts too
make e2e                        # demo drills on real kind clusters, about 5 minutes
make demo                       # build, then drill examples/demo/gapdrill.yaml
make snapshot                   # unsigned local build of every release archive (needs goreleaser)
go test ./internal/report -update   # regenerate testdata/demo.golden.md after a Markdown change, then review the diff
```

After a failed or interrupted e2e run, check for other active drills before
running `go run ./cmd/gapdrill cleanup` on the shared Docker host.

## Rules for changes

- **Every change reaches `main` through a pull request**, the maintainers'
  included. A PR merges once the required checks `ci-ok`, `e2e-ok` and `DCO`
  pass. `ci-ok` and `e2e-ok` are gate jobs that need every other job in their
  workflow. If you add a job, add it to the gate's `needs:`.
- **Sign off every commit** with `git commit -s`. The sign-off must match the
  commit author.
- **Some changes need a design doc first**: any change to the drill format,
  the report format or isolation. Write a short doc in `docs/design/`,
  following `0001-architecture.md`, and tick the box in the PR template.
- **Start every Go and shell file with the license header**:
  `// Copyright The gapdrill Authors` and
  `// SPDX-License-Identifier: Apache-2.0` (`#` comments in shell).
- **Put logic where it can be unit-tested.** Keep it in the pure packages
  (`spec`, `mirror`, `lockdown`, `dnsmon`, `report`, `learn`) and keep the
  I/O packages thin.
- **Keep the contracts**:
  - exit codes 0, 1, 2 and 3, kept apart so CI never mistakes a broken
    harness for a broken release;
  - strict decoding;
  - the `v1alpha1` schemas. Adding a field is fine. Renaming or removing one
    needs a design doc and a release note.
- **Write user-facing text as full sentences** that say what happened and
  why, for example "…looked up pypi.org, which doesn't resolve offline." Use
  the same wording in the README, the reports and the CLI help.
- **Update the docs in the same PR**: the README's drill reference, flags and
  Action inputs when they change, and `ROADMAP.md` and `docs/ideas.md` when a
  planned item ships.

## Recipes

### Add an operation type

1. In `internal/spec/spec.go`: add the struct, a field on `Operation`, a
   `Type…` constant, and cases in `Type()`, `typeCount()` and `Timeout()`.
2. In `internal/spec/validate.go`: add the type to both "set one of …"
   messages, plus its own field checks.
3. In `internal/ops/ops.go`: add a case in `Run`. If the operation depends on
   the cluster's state when it starts, expand it in `expand.go`.
4. Add unit tests in `spec` and `ops`, and add the type to the README's drill
   reference. A design doc is needed (drill format).

### Add a finding kind

1. In `internal/report/report.go`: add the constant and its place in
   `precedence`.
2. In `build.go`: create the finding in `findings()`.
3. In `markdown.go`: render it in `writeFindings`.
4. In `compare.go`: give it an id in `findingID`.
5. In `fix.go`: add a fix in `fixFor`, if one exists.
6. Regenerate the golden file. A design doc is needed (report format).

### Change the Action

- Keep the logic in `action/*.sh` and run shellcheck on it.
- Inputs reach the scripts as environment variables set in `action.yml`.
- e2e dogfoods the Action from the checkout (`uses: ./`) on the broken and
  fixed demos, and installs the latest release with `action/install.sh`.

## Releasing

1. Check that `main` is green.
2. On `main`, tag and push: `git tag -a vX.Y.Z -m "vX.Y.Z"`, then
   `git push origin vX.Y.Z`. A `-rc.N` tag publishes a pre-release.
3. The release workflow builds the binaries and SBOMs and signs
   `checksums.txt` keylessly with cosign. Its `verify` job then downloads the
   release, verifies it the way a user would, and runs the binary. Both jobs
   must pass.
4. The release notes are generated from the titles of the merged PRs, so
   write those titles for users.
5. **Release tags are protected: never move or delete one.** To fix a bad
   release, cut the next patch version.
