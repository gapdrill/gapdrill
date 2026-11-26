# gapdrill

**Find out whether your Helm release works without internet access, before your offline users do.**

gapdrill runs your chart in a local [kind](https://kind.sigs.k8s.io) cluster
that can't reach the internet and can pull images only from your offline
bundle. Then it runs the operations your users depend on (install, chart
tests, scheduled jobs, your own steps) and tells you which ones fail and why.

```console
$ gapdrill run -f examples/demo/gapdrill.yaml
gapdrill v0.1.0 · drill "demo" · 3 operations · 2 bundle images
[1/5] Creating an isolated network
[2/5] Copying 2 bundle images and creating the cluster
[3/5] Cutting the cluster off the internet
[4/5] Verifying the cluster is offline
[5/5] Running 3 operations
      ✗ install (22s)
      ✗ scheduled-cleanup (1m0s)
      ✓ chart-tests (3s)

✗ Failed: 2 of 3 operations failed offline.
  install            Job demo-post-install (Helm post-install hook) looked up pypi.org, which doesn't resolve offline.
  scheduled-cleanup  CronJob demo-cleanup needs docker.io/library/busybox:1.37.0, which isn't in the bundle.
  chart-tests        passed

Report: gapdrill-report/report.md, gapdrill-report/report.json
```

## Why

A chart can install cleanly in a disconnected cluster and still be broken
there, in code paths a basic install never touches:

- **Images pulled later.** A CronJob, or an operator, requests an image the
  bundle doesn't ship. Air-gapped Harvester clusters hit exactly this
  ([harvester/harvester#10007](https://github.com/harvester/harvester/issues/10007)).
- **Network calls that aren't image pulls.** A hook runs `pip install` or
  downloads a release asset. Every image is mirrored, and it still fails.
- **Pull policy.** `imagePullPolicy: Always` contacts a registry even when the
  image is already on the node.

Static scanners such as `zarf dev find-images` read image references out of
manifests, and they're worth running. They can't see images chosen at runtime,
or calls that aren't image pulls. gapdrill runs the release for real.

## Install

gapdrill needs **Docker** (Docker Desktop works) and **Helm** 3 or 4. You don't
need kind: it's built in.

Download a release for Linux or macOS, on amd64 or arm64:

```sh
VERSION=v0.1.0
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
curl -fsSLO "https://github.com/gapdrill/gapdrill/releases/download/${VERSION}/gapdrill_${VERSION#v}_${OS}_${ARCH}.tar.gz"
tar -xzf "gapdrill_${VERSION#v}_${OS}_${ARCH}.tar.gz" gapdrill
sudo install gapdrill /usr/local/bin/
```

Releases are signed and include SBOMs; [docs/releasing.md](docs/releasing.md#verify-a-release)
shows how to verify one. On macOS, download with `curl` as above: a file
downloaded in a browser is quarantined by Gatekeeper.

Or build it with Go 1.26 or later:

```sh
go install github.com/gapdrill/gapdrill/cmd/gapdrill@latest
```

## Quick start

Point gapdrill at a chart and the images your offline bundle ships:

```sh
gapdrill run --chart ./charts/my-chart --images images.txt
```

It installs the release, runs every CronJob once (including CronJobs the
release creates at runtime) and runs the chart's tests if it has any. Add
`--values`, `--set`, `--name` or `--namespace` as you would with Helm.

Learn mode and `gapdrill compare` are available in source builds after
v0.1.0. To try them from this checkout, run `make build` and use
`bin/gapdrill`.

Not sure what belongs in the bundle? A learn run lets the nodes pull images
the bundle lacks from their registries (the pods stay offline), then writes
every image the release pulled to `gapdrill-report/images.learned.txt`, with
the missing ones marked. Use the file as your image list:

```sh
gapdrill run --chart ./charts/my-chart --images images.txt --learn
```

To exercise more than that, write the drill to a file and add the operations
your users depend on:

```sh
gapdrill init --chart ./charts/my-chart --images images.txt   # writes gapdrill.yaml
gapdrill run                                                   # reads ./gapdrill.yaml
```

A drill file looks like this:

```yaml
# gapdrill.yaml
apiVersion: gapdrill/v1alpha1
kind: Drill
metadata:
  name: my-chart
bundle:
  imagesFile: images.txt        # the images your offline bundle ships, one per line
release:
  chart: ./charts/my-chart
  name: my-chart
  valuesFiles: [values-airgap.yaml]
operations:
  - name: install
    helmInstall: {timeout: 5m}
  - name: nightly-cleanup
    runCronJob: {name: my-chart-cleanup}
  - name: chart-tests
    helmTest: {}
```

Try the demo in [examples/demo](examples/demo): one drill fails on two
realistic bugs, and the other passes once they're fixed.

## Run it in GitHub Actions

```yaml
jobs:
  offline:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v7
      - uses: gapdrill/gapdrill@v0.1.0
        with:
          drill: gapdrill.yaml
```

The action downloads the gapdrill release it's referenced at, verifies its
signature and checksum, runs the drill, adds the report to the job summary and
uploads it as an artifact. The job fails when an operation fails offline. If
you pin actions to commit SHAs, pin this one to a release commit: the action
finds the release that commit belongs to. The runner needs Docker and Helm 3
or 4, for example from [azure/setup-helm](https://github.com/Azure/setup-helm).

| Input | Default | Meaning |
|---|---|---|
| `drill` | `gapdrill.yaml` | Path to the drill file |
| `report-dir` | `gapdrill-report` | Where to write `report.md` and `report.json` |
| `args` | | Extra arguments for `gapdrill run`, such as `--timeout 45m` |
| `version` | the action's release | The gapdrill release to run |
| `upload-report` | `true` | Upload the report as an artifact |
| `artifact-name` | `gapdrill-report` | The artifact's name; change it when a job runs more than one drill |

Outputs: `verdict` (`passed`, `failed`, `error` or `invalid`), `exit-code`,
and `report`, the path to `report.json`.

## Compare runs

```sh
gapdrill compare main-report/report.json gapdrill-report/report.json
```

prints the findings that are new or fixed since the base run, as Markdown for a
pull request comment. It exits 1 only when something new broke, so a pull
request that leaves an existing problem alone can still pass. Findings match by
their `id`, which depends on what they're about and the workload behind them,
not on pod names or times.

## How it works

1. **Prepare.** Starts a registry holding only your bundle's images, and
   creates a kind cluster beside it.
2. **Isolate.** Points containerd at that registry for every registry name,
   with no fallback upstream. Makes CoreDNS answer "no such host" for names
   outside the cluster and log each query. Installs egress rules in every node.
3. **Verify.** Fetches a probe URL from the node before and after isolation.
   If the cluster can still reach it, the drill stops: a test that isn't
   really offline proves nothing.
4. **Drill.** Runs your operations in order, watching pods, DNS queries and
   registry requests.
5. **Report.** Ties each finding to the operation it happened in and to its
   owner (a CronJob, a Helm hook), suggests the likely fix, lists the bundle images nothing pulled (so
   you can trim the bundle), and writes `report.md` and `report.json`. It
   also writes `egress.cdx.json`: every outside host each workload tried to
   reach, as a CycloneDX BOM that security tooling can read.

There's no root needed on the host, and gapdrill never touches your
kubeconfig or firewall. The full design is in
[docs/design/0001-architecture.md](docs/design/0001-architecture.md).

## Drill file reference

| Field | Meaning |
|---|---|
| `metadata.name` | Name of the drill (a lowercase DNS label). |
| `bundle.imagesFile` | File with one image reference per line; `#` starts a comment. Relative to the drill file. |
| `bundle.images` | More image references, added to the file's. |
| `cluster.nodeImage` | Override the kind node image (and so the Kubernetes version). |
| `release.chart` | Local chart directory, or any reference Helm accepts (`oci://…`, `repo/chart`). |
| `release.name`, `release.namespace` | Release name; the namespace defaults to the release name. |
| `release.valuesFiles`, `release.set` | Values for the release, as with `helm --values` and `--set`. |
| `operations[].helmInstall` | Install the release and wait (`timeout`, default `5m`). |
| `operations[].helmTest` | Run the chart's test hooks (`timeout`, default `3m`). With `ifPresent: true`, the operation is skipped when the chart has no tests. |
| `operations[].runCronJob` | Start a Job from a CronJob, like `kubectl create job --from=cronjob/…`, and wait for it (`name`, `namespace`, `timeout`). |
| `operations[].runCronJobs` | Run every CronJob in the cluster once, outside Kubernetes' own namespaces, including CronJobs created at runtime. Each is reported as `<operation>/<CronJob>` (`timeout` per CronJob, default `3m`). |
| `operations[].exec` | Run a command on the host, with `KUBECONFIG` set to the offline cluster. `GAPDRILL_RELEASE` and `GAPDRILL_NAMESPACE` identify the release (`command`, `timeout`). |
| `operations[].settle` | How long after the operation ends its evidence is still collected (default `5s`). |

Unknown fields are errors, so typos fail loudly. Check a file with
`gapdrill validate`. Offline, an image pull that fails keeps failing, so an
install, a chart's tests or a CronJob's Job stops 20 seconds after one of its
pods can't pull an image, rather than at its timeout. [Design doc 0002](docs/design/0002-zero-config-drills.md)
covers drills without a drill file.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Every operation passed. |
| 1 | An operation failed: the release has a problem. |
| 2 | The drill couldn't run: setup, isolation or verification failed. |
| 3 | Invalid input. |

In GitHub Actions, the Markdown report is also added to the job summary.

## Status

gapdrill is **alpha** (v0.1). The drill and report formats are `v1alpha1` and
may change before v1. Known gaps are listed in the
[design doc](docs/design/0001-architecture.md#isolation-model).

Where it's heading: the [vision](docs/vision.md), the [roadmap](ROADMAP.md)
and the [ideas](docs/ideas.md) not yet scheduled.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md). Commits
need a [DCO](https://developercertificate.org/) sign-off (`git commit -s`).
The [gapdrill agent skill](skills/gapdrill/SKILL.md) covers running drills,
reading reports and contributing to the code. Copy the whole
`skills/gapdrill` directory into your agent's skills directory to install it.

Using gapdrill in your release pipeline? Add yourself to
[ADOPTERS.md](ADOPTERS.md).

This project follows the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md).

## License

[Apache License 2.0](LICENSE).
