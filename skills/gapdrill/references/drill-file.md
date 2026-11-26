# Drill file reference (`gapdrill/v1alpha1`)

Read this when you're writing or fixing a drill file. Check the file with
`gapdrill validate -f FILE` before running it. That command needs no Docker,
and it reports every problem at once, each with its field path:

```text
gapdrill: gapdrill.yaml:
  operations[0].helmTest: needs a helmInstall operation before it
  operations[2].name: "install" is used by an earlier operation
```

## Example with every operation type

```yaml
apiVersion: gapdrill/v1alpha1
kind: Drill
metadata:
  name: my-chart                     # lowercase DNS label
bundle:
  imagesFile: images.txt             # relative to this file
  images:                            # added after the file's images
    - docker.io/library/busybox:1.37.0
release:
  chart: ./charts/my-chart           # or oci://..., or repo/chart
  name: my-chart                     # at most 53 characters
  namespace: my-chart                # default: the release name
  valuesFiles: [values-airgap.yaml]  # relative to this file
  set:
    replicaCount: 1
operations:
  - name: install
    helmInstall: {timeout: 5m}
  - name: nightly-cleanup
    runCronJob: {name: my-chart-cleanup, timeout: 2m}
  - name: every-cronjob
    runCronJobs: {timeout: 3m}       # per CronJob
  - name: restore-backup
    exec:
      command: ["./scripts/restore.sh"]
      timeout: 10m
    settle: 15s
  - name: chart-tests
    helmTest: {ifPresent: true}
```

## Top-level fields

| Field | Required | Meaning |
|---|---|---|
| `apiVersion` | yes | `gapdrill/v1alpha1` |
| `kind` | yes | `Drill` |
| `metadata.name` | yes | The drill's name: a lowercase DNS label (`a-z`, `0-9` and `-`, at most 63 characters). |
| `cluster.nodeImage` | no | The kind node image, and so the Kubernetes version. The default is the image of the kind version gapdrill is built with. |
| `bundle.imagesFile` | this, `bundle.images` or both | The image list file, relative to the drill file (format below). |
| `bundle.images` | this, `bundle.imagesFile` or both | More image references, added after the file's. Duplicates are dropped. |
| `release.chart` | yes | A local chart directory, relative to the drill file, or any reference Helm accepts. |
| `release.name` | yes | A lowercase DNS label of at most 53 characters. |
| `release.namespace` | no | Defaults to `release.name`. Created if it doesn't exist. |
| `release.valuesFiles` | no | As `helm --values`, relative to the drill file. |
| `release.set` | no | A map, passed as `helm --set key=value`. |
| `operations` | yes | At least one. They run in order. |

## Operations

Each operation has a unique `name` and exactly one of the types below. It can
also have `settle`: how long after the operation ends its evidence is still
blamed on it (default `5s`). Raise it when a failure shows up a little later,
for example a pod a controller starts after the step returns.

| Type | What it does | Fields (defaults) |
|---|---|---|
| `helmInstall` | Runs `helm upgrade --install --create-namespace --wait`. | `timeout` (`5m`) |
| `helmTest` | Runs `helm test --logs`. It needs a `helmInstall` earlier in the drill. | `timeout` (`3m`); `ifPresent: true` skips the operation when the chart has no test hooks |
| `runCronJob` | Creates a Job from the CronJob's template, as `kubectl create job --from=cronjob/NAME` does, then waits for it. | `name` (required), `namespace` (the release namespace), `timeout` (`3m`) |
| `runCronJobs` | Runs every CronJob in the cluster when the operation starts, including ones the release created at runtime. | `timeout` per CronJob (`3m`) |
| `exec` | Runs a command on the host. It fails on a non-zero exit. | `command` (required), `timeout` (`3m`) |

More on `runCronJobs`:

- It leaves out CronJobs in `kube-system`, `kube-public`, `kube-node-lease`
  and `local-path-storage`.
- Each CronJob is reported as its own operation: `<name>/<cronjob>`, or
  `<name>/<namespace>/<cronjob>` outside the release namespace.
- It's `skipped` when the cluster has no CronJobs.

More on `exec`:

- `command` is an argv list, not a shell line. For pipes, `&&` or variables,
  use `["sh", "-c", "..."]`.
- It runs in the drill file's directory (with `--chart`, the current
  directory), with these variables set: `KUBECONFIG` (the offline cluster),
  `GAPDRILL_RELEASE` and `GAPDRILL_NAMESPACE`.
- Any tool it calls, such as `kubectl`, must be on the host.
- It finishes when the command exits. To let a controller act first, make the
  command wait, for example with `kubectl wait --for=condition=Ready ...`.

## Rules worth knowing

- Decoding is strict: an unknown field is an error, so typos fail loudly.
- Durations are strings such as `"90s"`, `"5m"` or `"1h"`, and must be
  positive. A bare number such as `timeout: 90` is an error.
- Put `runCronJob` and `runCronJobs` after the `helmInstall` that creates the
  CronJobs. Validation doesn't check this order. Placed too early, a
  `runCronJob` fails because its CronJob doesn't exist yet, and a
  `runCronJobs` is skipped because there are none.
- An operation's `timeout` bounds that operation. `gapdrill run --timeout`
  bounds the whole drill.

## Image list format

```text
# The images the offline bundle ships. '#' starts a comment, anywhere on a line.
docker.io/library/busybox:1.36.1
ghcr.io/example/app@sha256:<digest>   # digest references copy the whole multi-platform index
quay.io/example/tool:v2               # tag references copy only linux/<node architecture>
```

- One reference per line. A line with two words is an error.
- Names are normalized the way container runtimes do: `busybox`,
  `docker.io/busybox:latest` and `index.docker.io/library/busybox` are the same
  image.
- The `images.learned.txt` file a learn run writes is already in this format.

## What `init` and `run --chart` use

`gapdrill init --chart CHART --images FILE` writes this drill (with comments),
and `gapdrill run --chart` runs the same one without writing it:

```yaml
operations:
  - name: install
    helmInstall: {}
  - name: cronjobs
    runCronJobs: {}
  - name: chart-tests
    helmTest:
      ifPresent: true
```

- The release name comes from the chart's `Chart.yaml`, or else from the
  reference's last path element without its version.
- Paths in the written file are relative to the file.
