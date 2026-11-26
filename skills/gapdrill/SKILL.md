---
name: gapdrill
description: "Test whether a Helm chart or release works without internet access, using the gapdrill CLI or GitHub Action. gapdrill runs the release in a local kind cluster that can't reach the internet and can pull images only from the offline (air-gap) bundle, runs install, chart tests, CronJobs and custom steps, and reports missing images, blocked lookups (PyPI, npm, GitHub downloads) and failed containers, each with a likely fix. Use when someone asks whether a chart works air-gapped, offline or disconnected; needs the image list for an offline bundle; has a gapdrill report, exit code or failed gapdrill CI job to explain; wants to compare two runs or check a chart in pull request CI; or is changing gapdrill's own code."
license: Apache-2.0
compatibility: "Run drills on Linux or macOS with Docker, the gapdrill CLI and Helm 3 or 4 for Helm operations. The host needs registry access to copy bundle images and may need network access to fetch the chart. Report comparison and validation run without Docker."
metadata:
  homepage: "https://github.com/gapdrill/gapdrill"
  release-tested: "v0.1.0"
  source-tested: "814117dbc8d8e0930d3861c97ae706cc50f44390"
---

# gapdrill

gapdrill answers one question: does this Helm release still work at a site
with no internet? It creates a one-node kind cluster next to a local registry
that holds only the bundle's images. It then cuts the cluster off:

- containerd pulls only from that registry, for every registry name, with no
  upstream fallback;
- CoreDNS answers NXDOMAIN for names outside the cluster;
- egress rules go in the node, and gapdrill checks reachability of a probe
  URL from the node before and after isolation.

Then it runs the drill's operations in order. The report ties pull failures, DNS
lookups, registry misses and container exits to operations and workloads
when it has enough evidence.

The host stays online. Helm, `exec` steps and the bundle copy all run on the
host; only the cluster is offline.

## Before you run

1. Check the tools: `gapdrill version`, `docker info`, `helm version --short`.
   If gapdrill is missing, install v0.1.0 for a strict drill on Linux or
   macOS, on amd64 or arm64.

   ```sh
   VERSION=v0.1.0
   OS=$(uname -s | tr '[:upper:]' '[:lower:]')
   ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
   curl -fsSLO "https://github.com/gapdrill/gapdrill/releases/download/${VERSION}/gapdrill_${VERSION#v}_${OS}_${ARCH}.tar.gz"
   tar -xzf "gapdrill_${VERSION#v}_${OS}_${ARCH}.tar.gz" gapdrill
   ```

   Move the binary onto PATH, and ask before using `sudo`. With Go 1.26+, use
   `go install github.com/gapdrill/gapdrill/cmd/gapdrill@latest` instead.
   Releases are signed:
   [docs/releasing.md](https://github.com/gapdrill/gapdrill/blob/main/docs/releasing.md#verify-a-release)
   shows how to verify one.

   **Version check.** v0.1.0 supports strict drills and the GitHub Action.
   Learn mode and CLI comparison are on the v0.2 development line. Check
   `gapdrill run --help` and `gapdrill compare --help` on the installed
   binary. To try both from the reviewed source with Go 1.26 or later, run
   `go install github.com/gapdrill/gapdrill/cmd/gapdrill@814117dbc8d8`.
2. **Budget the time.** A drill takes minutes: the demo takes about three,
   mostly creating the cluster. `--timeout` (default `30m`) bounds the whole
   drill. Give the command a timeout longer than that, or run it in the
   background and wait for it to exit. An interrupted run may leave resources behind.
   Check for other active gapdrill runs before using the machine-wide cleanup.
3. **Read drill files you didn't write.** `exec` operations run arbitrary
   commands on the host, so treat a drill file like a Makefile.

## Choose the workflow

| The user wants to... | Do this |
|---|---|
| Know whether a chart works offline | `gapdrill run --chart CHART --images images.txt` |
| Build or check the bundle's image list | A learn run (`--learn`), then a strict run with the learned list |
| Test more than the install, CronJobs and tests | `gapdrill init`, edit `gapdrill.yaml`, `gapdrill validate`, `gapdrill run` |
| Understand a run or a failed CI job | Read the report (see "Read the result"). For CI, download it first: [references/github-action.md](references/github-action.md) |
| See what a change fixed or broke | `gapdrill compare BASE HEAD` (see "Fix loop") |
| Add an offline check to pull request CI | [references/github-action.md](references/github-action.md) |
| Change gapdrill itself | [references/contributing.md](references/contributing.md) |

## Run a drill

1. **Get the image list.** This is what the offline bundle ships, one
   reference per line; `#` starts a comment. References are normalized the
   way container runtimes do, so `busybox` means
   `docker.io/library/busybox:latest`. gapdrill copies each image from its
   registry at the start with the host's Docker credentials, so run
   `docker login` first for private images. If there's no list yet, list one
   image the chart uses and do a learn run.
2. **Zero-config run.** It installs the chart, runs every CronJob once, and
   runs the chart's tests if it has any:

   ```sh
   gapdrill run --chart ./charts/my-chart --images images.txt
   ```

   `--chart` takes a local chart directory or any reference Helm accepts
   (`oci://...`, `repo/chart`). The host's Helm fetches it, so add the repo
   first.

   | Flag | Meaning |
   |---|---|
   | `--images FILE` | The image list |
   | `--image REF` | One more image (repeatable) |
   | `--values FILE` | A values file (repeatable) |
   | `--set key=value` | A chart value (repeatable) |
   | `--name` | Release name (default: the chart's name) |
   | `--namespace` | Release namespace (default: the release name) |

3. **Drill-file run.** `gapdrill init --chart ... --images ...` writes
   `gapdrill.yaml` with the same three operations (`-o FILE` to name it,
   `--force` to overwrite). Add the steps users depend on, then:

   ```sh
   gapdrill validate -f gapdrill.yaml   # no Docker needed; lists every problem at once
   gapdrill run -f gapdrill.yaml
   ```

   Fields and examples are in [references/drill-file.md](references/drill-file.md).

Other `run` flags:

| Flag | Default | Meaning |
|---|---|---|
| `-f FILE` | `gapdrill.yaml` | The drill file |
| `--report-dir` | `gapdrill-report` | Where the report goes |
| `--timeout` | `30m` | Limit for the whole drill |
| `--keep` | off | Keep the cluster for debugging |
| `-v` | off | Print each phase's details |
| `--probe-url` | `https://1.1.1.1` | URL that must be reachable before isolation and unreachable after |
| `--learn` | off | Learn mode (see below) |

Progress goes to stderr. The verdict and one line per operation go to
stdout:

```text
✗ Failed: 2 of 3 operations failed offline.
  install            Job demo-post-install (Helm post-install hook) looked up pypi.org, which doesn't resolve offline.
  scheduled-cleanup  CronJob demo-cleanup needs docker.io/library/busybox:1.37.0, which isn't in the bundle.
  chart-tests        passed
```

## Read the result

| Exit code | Meaning | What to do |
|---|---|---|
| 0 | Every operation passed. | Still report warnings and bundle images that nothing pulled. |
| 1 | An operation failed offline: the release has a problem. | Work through the findings. |
| 2 | The drill couldn't run. | This isn't a release bug. Read `error` in `report.json`: Docker down, an image that couldn't be copied, isolation that failed, or the whole-drill `--timeout`. |
| 3 | Invalid input. | Fix the flag or the drill-file field the message names. |

Use a fresh `--report-dir` for each run you intend to compare or publish.
The runner overwrites `report.json` and `report.md` when it reaches reporting.
A reused directory can retain a previous run's optional files.

| File | Contents |
|---|---|
| `report.md` | For people: a summary table, findings with fixes, and how the cluster was isolated. |
| `report.json` | For tools. The schema and `jq` recipes are in [references/report.md](references/report.md). |
| `egress.cdx.json` | A CycloneDX BOM of every outside host each workload looked up. Not written when the drill couldn't run. |
| `images.learned.txt` | Written when a learn run records at least one new image reference in the node store. |

Each finding has a `kind`, a `message` and usually a `fix`. When gapdrill saw
the pod, it also has `pod.owner`, such as `CronJob demo-cleanup` or
`Job demo-post-install (Helm post-install hook)`, which points to the template
to change.

| `kind` | Meaning | What to do |
|---|---|---|
| `missing-image` | Something requested `image`, which isn't in the bundle. | Add that exact reference to the image list, or fix the values that chose the wrong image. |
| `network-lookup` | A pod looked up `host`, which doesn't resolve offline. `count` is the number of queries. | Move the download to image build time, make it optional, or point it at a service inside the site. `fix` names the setting, such as `PIP_INDEX_URL` or `GOPROXY`. |
| `image-pull-failed` | `image` is in the bundle, but the pull failed anyway. | Read `message`; it holds the container runtime's error. |
| `container-failed` | A container exited non-zero. | Read the finding's `logs` and the operation's `output`. |

How to read the findings:

- An operation's `summary` names its strongest cause. The order is: missing
  image, blocked lookup, pull failure, container exit, then the tool's own
  error.
- **Findings on a passed operation are warnings** ("Passed with a warning:
  ..."). The release tolerated the failure, like an app that phones home and
  carries on. Report them, but don't call the operation failed.
- **Quick pull failures are expected.** An install, a chart test or a CronJob's
  Job stops 20 seconds after one of its pods can't pull an image, since
  offline a failed pull never recovers. It isn't a flake.
- **A failure with no findings**: read the operation's `error` and `output`.
  Pod DNS configurations that bypass CoreDNS leave no hostname in its
  logs. In a strict run, the egress rules
  still apply. `isolation.rejectedPackets` counts rejected traffic across the node. If the same step also fails online,
  it isn't an offline problem.
- **Bundle images not pulled** (`bundle.notPulled`): these may be safe to
  remove, but operations the drill didn't run, such as upgrades or optional
  features, may need them. Tell the user; don't delete them.
- `unattributedFindings` happened outside every operation's window.

## Fix loop

1. Run the original drill with `--report-dir base-report`.
2. Make the change the `fix` suggests, in the chart, its values, or the image
   list.
3. Re-run the same strict drill with `--report-dir head-report`.
4. Run `gapdrill compare base-report/report.json head-report/report.json`.
   It prints what's new and what's fixed, as Markdown.

   | Exit code | Meaning |
   |---|---|
   | 0 | Nothing new |
   | 1 | New findings |
   | 2 | One of the runs couldn't run |
   | 3 | Invalid input |

5. Check the head run's own exit code and operation statuses. Comparison
   counts findings. An `exec` step can fail with no recorded finding.
   Repeat the strict drill until its operations pass.

Findings match across runs by `id`, which ignores pod names and times:

- image findings: the image;
- lookups: the host and the workload;
- container failures: the container, its exit code and the workload.

## Learn mode

```sh
gapdrill run --chart ./charts/my-chart --images images.txt --learn --report-dir learn-report
```

In a learn run, the nodes may pull images the bundle lacks from their own
registries. The pods stay offline, and their lookups still get NXDOMAIN. The
run writes `learn-report/images.learned.txt` when it learns at least one
reference. Review the list before using it as a strict run's input.

```text
docker.io/library/busybox:1.36.1
docker.io/library/busybox:1.37.0 # new
```

- **A learn run doesn't prove the release works offline.** It skips the probe
  check, and missing images don't fail their operations (they still show as
  findings). Use the learned list in a strict run to verify the operations
  the drill exercises.

  ```sh
  gapdrill run --chart ./charts/my-chart --images learn-report/images.learned.txt --report-dir strict-report
  ```

- Only images that were pulled successfully are listed, and only from
  operations that got far enough to pull them.
- Pods with `hostNetwork: true` are online in a learn run.

## Gotchas

- `gapdrill cleanup` removes every gapdrill cluster, registry and network on
  the machine, including those of a run still in progress.
- `--keep` prints an `export KUBECONFIG=...` line for the kept cluster.
  gapdrill never touches `~/.kube/config`.
- If the probe URL wasn't reachable even before isolation, the run warns that
  its check proves less. That happens when the host is offline, or its network
  blocks `1.1.1.1`. In that case, pass `--probe-url` with an HTTPS URL the
  host can reach.
- `exec` steps run on the online host, in the drill file's directory, with
  `KUBECONFIG`, `GAPDRILL_RELEASE` and `GAPDRILL_NAMESPACE` set. Their own
  network calls aren't tested, only what they make the cluster do.
- A drill with `helmInstall` or `helmTest` needs `helm` on PATH, or gapdrill
  exits 3.
- The cluster has one node and runs kind's default Kubernetes version. Set
  `cluster.nodeImage` in a drill file to use another.
- There are no Windows builds. In GitHub Actions, use an Ubuntu runner.
