# 0001: gapdrill architecture

- **Status:** Accepted for v0.1
- **Last updated:** 2026-11-24

## Summary

gapdrill runs a Helm release in a local Kubernetes cluster that has no internet
access and can pull images only from the release's offline bundle. It then
exercises the operations users depend on (install, tests, scheduled jobs,
custom steps) and reports which ones fail and why.

## Problem

A chart can install cleanly in a disconnected cluster and still be broken
there. The failures appear later, in code paths a basic install never touches:

- **Images pulled after install.** Air-gapped Harvester clusters hit a cleanup
  CronJob, created at runtime by the bundled Rancher, that requests an image
  the ISO doesn't ship ([harvester/harvester#10007]).
- **Network calls that aren't image pulls.** An install hook that runs
  `pip install` or downloads a release asset works online and fails offline,
  even when every image is mirrored.
- **Pull policy.** `imagePullPolicy: Always` (the default for `:latest`)
  contacts a registry even when the image is already on the node.

Static scanners such as `zarf dev find-images` read image references out of
rendered manifests. They can't see images an operator chooses at runtime or
network calls that aren't image pulls. Testkube's air-gap guide makes the same
point to operators: "A Control Plane that starts cleanly does not prove the
bundle is complete" ([Testkube air-gap guide]). Maintainers who care about
offline users either write a project-specific harness (OpenCost AI's
`airgap-e2e` workflow is one) or don't test at all.

## Goals

1. **One file, one command.** A maintainer describes the release, its bundle and
   the operations to exercise; gapdrill does everything else.
2. **Faithful offline simulation.** Nothing in the cluster can reach the
   internet, and images come only from the bundle.
3. **Actionable results.** Each failed operation names its cause: a missing
   image and what asked for it, a blocked lookup, or a failing container and
   its logs.
4. **Trustworthy results.** The cluster is verified to be offline before any
   operation runs, and "your release failed" is never confused with "the drill
   itself broke".
5. **Same behaviour everywhere.** Runs on a laptop (macOS or Linux, including
   Docker Desktop) and in CI, without root on the host and without touching the
   user's kubeconfig or firewall.

## Non-goals (v0.x)

- Building or packaging bundles. Zarf, Hauler and project scripts already do
  this; gapdrill consumes their output.
- Static image discovery as the primary signal (see
  [Relationship to existing tools](#relationship-to-existing-tools)).
- Multi-node topologies, clusters other than kind, Windows nodes.
- Partial connectivity (proxies, allow-listed egress). A possible later feature.

## User experience

A drill file sits next to the chart:

```yaml
apiVersion: gapdrill/v1alpha1
kind: Drill
metadata:
  name: demo
bundle:
  imagesFile: images.txt          # one image reference per line
release:
  chart: ./chart                  # local path, or any reference Helm accepts
  name: demo
  namespace: demo
operations:
  - name: install
    helmInstall: {timeout: 3m}
  - name: scheduled-cleanup
    runCronJob: {name: demo-cleanup}
  - name: chart-tests
    helmTest: {}
```

```text
gapdrill run [-f gapdrill.yaml] [--report-dir DIR] [--keep] [--timeout 30m]
gapdrill validate [-f gapdrill.yaml]
gapdrill cleanup
gapdrill version
```

**Exit codes:**

| Code | Meaning |
|---|---|
| 0 | Every operation passed. |
| 1 | One or more operations failed. The release has a problem. |
| 2 | The drill couldn't run: setup, isolation or verification failed. |
| 3 | Invalid input (drill file, bundle list, flags). |

**Outputs:** `report.md` for people and `report.json` (schema
`gapdrill/report/v1alpha1`) for tools. When `$GITHUB_STEP_SUMMARY` is set, the
Markdown is appended to it.

## Architecture

```text
prepare ─┬─ create a Docker network for the run
         ├─ start the bundle registry ── copy bundle images into it ──┐  (in parallel)
         └─ create the kind cluster on that network ──────────────────┤
isolate  ── containerd mirror config ── CoreDNS config ── egress rules ┘
verify   ── probe the internet before and after isolation ── check the rules are installed
drill    ── run the operations in order, observing pods, DNS queries and registry requests
report   ── attribute evidence to operations ── write report.md and report.json
cleanup  ── delete the cluster, registry and network (unless --keep)
```

| Package | Responsibility |
|---|---|
| `spec` | Drill schema, strict decoding, defaults, validation |
| `mirror` | Image reference normalisation; containerd `hosts.toml` generation |
| `lockdown` | iptables/ip6tables rules, rule checks, counters |
| `dnsmon` | CoreDNS configuration and query-log parsing |
| `registry` | Bundle registry lifecycle, image copy, access-log parsing |
| `cluster` | kind cluster lifecycle and node exec |
| `observe` | Watches pods and DNS logs; records evidence |
| `ops` | Operation runners (Helm install/test, CronJob, exec) |
| `report` | Attribution, diagnosis, Markdown and JSON rendering |
| `runner` | Orchestrates the phases, cleanup and exit codes |

The pure packages (`spec`, `mirror`, `lockdown`, `dnsmon`, `report`, and the
parsers in `registry`) have no I/O and are covered by unit tests. The I/O
packages are thin and covered by the end-to-end tests.

## Isolation model

| Path out of the cluster | How it's closed | How it's verified |
|---|---|---|
| Pod → internet | `REJECT` in each node's `FORWARD` chain for anything outside the pod, service and node networks | Rule presence check |
| Node process → internet | Same chain, hooked into `OUTPUT` | `curl` from the node succeeds before isolation and fails after |
| IPv6 | The same rules through `ip6tables`; only loopback and the node network are allowed | Rule presence check |
| Host services via the Docker gateway (for example, a proxy on the laptop) | New connections to the network gateway are rejected | Rule presence check |
| Image pulls → upstream registries | containerd `hosts.toml` for every bundle registry and for `_default`, with `server` set to the bundle registry. containerd otherwise falls back to the upstream registry after trying the mirrors. | Registry access log |
| Pod DNS for outside names | CoreDNS answers `NXDOMAIN` for anything outside the cluster domain and logs the query | Query log |

Allowed traffic uses `RETURN`, not `ACCEPT`, so kube-proxy and kindnet rules
further down the chains still apply.

**Why the rules live inside the nodes and not on the host:** no root on the
host; identical behaviour on Docker Desktop, where the macOS firewall can't
see container traffic; no way to break the host's networking; and cleanup is
just deleting the cluster.

**Known gaps in v0.1:**

- Pods using `hostNetwork` or `dnsPolicy: Default` resolve names through the
  node's resolver, so their lookups aren't logged. Their connections are still
  rejected.
- Blocked connections are counted per run, not yet attributed to pods.
- Pod-level egress is checked by rule presence, not yet by a probe pod.

## Evidence and attribution

| Source | What it yields |
|---|---|
| Pod status | Image pull failures with the exact image reference; container exits; log tails |
| CoreDNS query log | Outside names each pod looked up (client IP → pod via an IP history) |
| Registry access log | Manifests requested but not in the bundle |
| iptables counters | Number of rejected packets |

Each operation has a window, and the windows partition the drill: an
operation owns everything from its start until the next operation starts.
The runner waits out each operation's settle period (5s by default) before
starting the next, so late effects still land in the right window. A second
of clock slack applies only before the first operation and after the last one
settles; anything outside every window is reported as unattributed. (An
earlier draft let windows overlap by that slack, which blamed a CronJob's pull
failure on the install before it. The end-to-end test caught it.) Owners are resolved when a pod is
first seen (Pod → Job → CronJob, Pod → ReplicaSet → Deployment, Helm hook
annotations), so reports say "CronJob demo-cleanup" rather than a pod hash.

A failed operation's one-line diagnosis takes the strongest cause it has, in
this order: missing image, blocked lookup, image pull failure, container
failure, then the tool's own error text. Passing operations still list
findings, as warnings. An app that phones home but tolerates failure is worth
knowing about.

## Key decisions

1. **kind as a Go library, not a binary.** One thing to install; the node image
   is pinned by kind's version; the kubeconfig goes to a temporary file, never
   `~/.kube/config`.
2. **A local registry plus containerd mirror config, not `kind load`.**
   Preloaded images and a cut network produce false failures for
   `imagePullPolicy: Always`, and a registry 404 names each missing image.
3. **One registry, with the upstream host kept in the repository path**
   (`override_path`). The same repository on two registries can't collide.
4. **`NXDOMAIN` instead of dropping DNS.** Disconnected sites answer "no such
   host" quickly, so failures are fast and the names are logged.
5. **Helm through its CLI, not the SDK.** Uses the maintainer's Helm (v3 or v4)
   with its exact behaviour, and doesn't tie gapdrill to a Helm major version.
6. **Bundle images are copied with the Docker credential chain, retried
   anonymously on 401 or 403.** Stale credentials are common and shouldn't
   break public bundles.
7. **Only the node's platform is copied for tag references; the full index for
   digest references.** Runs stay fast on small machines without breaking
   digest-pinned charts.
8. **Separate exit codes for "release failed" and "drill failed".** CI must
   never report a broken harness as a broken release, or the reverse.
9. **Versioned drill and report schemas (`v1alpha1`) with strict decoding.**
   Typos in drill files fail loudly, and consumers can rely on report fields.

## Relationship to existing tools

- **Zarf and Hauler** build and deploy bundles; gapdrill tests what they
  produce. Zarf packages and Hauler stores as bundle inputs are on the roadmap.
- **`zarf dev find-images`** is a fast static check, worth running first.
  gapdrill covers what it can't see.
- **chart-testing (`ct`)** lints and installs charts with internet access.
  gapdrill is the offline counterpart for release pipelines.
- **Replicated Compatibility Matrix** offers hosted, commercial air-gap
  environments. gapdrill is local and open source.
- **Project harnesses** such as OpenCost AI's `airgap-e2e` workflow are what
  gapdrill aims to replace with a reusable step.

## Security considerations

- kind runs privileged node containers. gapdrill adds no host privileges and
  never edits host firewall rules or the user's kubeconfig.
- Registry credentials are used only on the host to copy bundle images; they
  never enter the cluster.
- The bundle registry is published on `127.0.0.1` only.
- Default images are pinned by digest, and CI actions by commit SHA.
- `exec` operations run on the host with the drill's `KUBECONFIG`. Drill files
  are trusted input, like Makefiles.

## Testing strategy

- **Unit tests:** drill decoding and validation, image normalisation, mirror
  config, iptables rules, Corefile, DNS and registry log parsing, attribution,
  report rendering (golden files).
- **End-to-end tests** (CI, kind on GitHub runners, Helm v3 and v4): the demo
  drill must fail with the expected findings, and the fixed drill must pass.

## Milestones

- **v0.1:** this design. CLI; Helm install and test, CronJob and exec
  operations; Markdown and JSON reports; demo.
- **v0.2:** GitHub Action; signed release binaries with SBOMs; probe pod;
  per-pod attribution of blocked connections.
- **v0.3:** bundle inputs (OCI layout or tarball, Zarf package, Hauler store);
  operator workflows (apply manifests, wait for conditions).
- **v0.4:** static pre-check comparing rendered images with the bundle;
  multi-architecture runs.

## Open questions

1. Should drills support allow-listed egress, for sites with a proxy?
2. Should gapdrill provide an in-cluster package mirror (PyPI, npm) so that
   "fixed" configurations can be tested?
3. What stability promise should the report schema make before v1?

[harvester/harvester#10007]: https://github.com/harvester/harvester/issues/10007
[Testkube air-gap guide]: https://docs.testkube.io/articles/install/air-gapped
