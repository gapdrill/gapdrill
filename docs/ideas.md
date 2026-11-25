# Ideas

Proposals that aren't on the [roadmap](../ROADMAP.md) yet. Each says what
problem it solves, what's new about it and roughly how big it is. They're
grouped by the three jobs in the [vision](vision.md): prove, explain and fix,
and show.

Have an idea? Open an issue labelled `idea`, or add it here in a pull request.
When we commit to one, it moves to the roadmap, with a design doc if it
changes the drill or report formats or how isolation works.

**Effort:** S is days, M is a few weeks, L is a project of its own.

## Summary

| # | Idea | Job | Effort | Status |
|---|---|---|---|---|
| 1 | [Zero-config drills](#1-zero-config-drills) | Prove | M | Shipped in v0.1 |
| 2 | [Pull request gate](#2-pull-request-gate) | Show | M | Next up |
| 3 | [Offline readiness index](#3-offline-readiness-index) | Show | M | Next up |
| 4 | [Learn mode](#4-learn-mode-write-the-bundle-from-what-ran) | Explain and fix | M | Next up |
| 5 | [Upgrade and rollback drills](#5-upgrade-and-rollback-drills) | Prove | M | Proposed |
| 6 | [Restricted networks](#6-restricted-networks-proxies-and-allow-lists) | Prove | M | Proposed |
| 7 | [Egress inventory](#7-egress-inventory) | Show | S | Proposed |
| 8 | [Check Artifact Hub image lists](#8-check-artifact-hub-image-lists) | Show | S | Proposed |
| 9 | [Fix hints](#9-fix-hints) | Explain and fix | S–M | Proposed |
| 10 | [Readiness badge](#10-readiness-badge) | Show | S | Proposed |
| 11 | [Drills in an existing cluster](#11-drills-in-an-existing-cluster) | Prove | L | Research |
| 12 | [Fail fast during install](#12-fail-fast-during-install) | Prove | S–M | Proposed |
| 13 | [Unused bundle images](#13-unused-bundle-images) | Explain and fix | S | Proposed |

"Next up" marks the ideas that matter most for adoption: they remove setup,
put gapdrill in every pull request and produce public evidence.

## 1. Zero-config drills

**Problem.** Writing a drill file is the main step between hearing about
gapdrill and seeing a result. Maintainers also forget operations they didn't
write themselves, such as a CronJob that comes from a subchart.

**Idea.** `gapdrill run --chart ./chart --images images.txt`, with no drill
file. gapdrill installs the release, runs every CronJob the release created
once, and runs `helm test` if the chart has tests. Given an earlier chart
version, it also drills the upgrade (idea 5).

**What's new.** The release's own objects define the drill, so new CronJobs
and tests are covered without anyone remembering to add them.

**Effort.** M.

**Status.** Shipped in v0.1, without the upgrade step: see
[design doc 0002](design/0002-zero-config-drills.md).

## 2. Pull request gate

**Problem.** Offline bugs arrive in ordinary changes: a new image, a hook that
downloads something. A periodic audit finds them weeks later.

**Idea.** In the GitHub Action, compare the drill with the last result from
the default branch and comment only on what the pull request changed: "This
PR adds `busybox:1.37.0` to CronJob `cleanup`, and it isn't in the bundle."

**What's new.** Offline readiness reviewed like any other diff, when the
change is made.

**Effort.** M. Needs findings with stable IDs in `report.json`.

## 3. Offline readiness index

**Problem.** People who run disconnected sites find out whether a popular chart
works offline by trying it.

**Idea.** Drill widely used charts every week and publish the results as a
static site, with each finding linked to the upstream issue we opened about
it. Maintainers hear about a finding before it's published.

**What's new.** A public, evidence-based answer to "does this chart work
offline?" It's also gapdrill's largest real-world test suite: every chart that
trips up the drill itself shows us a bug.

**Effort.** M, then ongoing.

## 4. Learn mode: write the bundle from what ran

**Problem.** Bundle image lists are usually built by rendering the chart and
collecting image references. That misses images chosen at runtime, such as the
ones an operator or a Job creates.

**Idea.** A learn run lets image pulls that miss the bundle fall through to the
upstream registry, and records them. DNS and all other egress stay blocked.
gapdrill then writes the complete list as `images.txt`, a Zarf package
component or a Hauler manifest.

**What's new.** The list comes from a full lifecycle (install, hooks,
scheduled jobs, tests, upgrade), not from rendered templates.

**Related work.** `zarf dev find-images` finds images in rendered manifests.

**Effort.** M.

## 5. Upgrade and rollback drills

**Problem.** Upgrades bring new hooks, migration jobs and images, and offline
sites upgrade less often, so each upgrade carries more change.

**Idea.** Install version N from its bundle, upgrade to N+1 from the new
bundle, run the release's operations, then roll back.

**What's new.** chart-testing already tests in-place upgrades
(`ct install --upgrade`), with internet access. This runs the same path
offline, with each version's own bundle.

**Effort.** M.

## 6. Restricted networks: proxies and allow-lists

**Problem.** Many disconnected sites aren't fully air-gapped: traffic goes
through a proxy, or a few hosts are allowed. Components that ignore proxy
settings, or a `NO_PROXY` that misses the cluster's internal addresses, break
there and nowhere else.

**Idea.** Drill profiles where only an HTTP proxy, or a list of hosts, is
reachable. Report which workloads bypassed the proxy and which requests failed
through it.

**What's new.** Proxy compliance tested as a property of the release.

**Effort.** M. This is open question 1 in the
[design doc](design/0001-architecture.md#open-questions).

## 7. Egress inventory

**Problem.** Security reviewers at regulated sites ask what a release contacts.
The answer usually comes from documentation, not evidence.

**Idea.** Write every host each workload tried to reach during the drill as a
CycloneDX BOM, using its `services` and `endpoints` fields, so the result plugs
into existing supply-chain tooling.

**What's new.** An outbound-network inventory based on observed behaviour, in
a standard format.

**Effort.** S. Drills already record the lookups.

## 8. Check Artifact Hub image lists

**Problem.** Artifact Hub takes a chart's image list from a dry-run install
with default values, or from the chart's `artifacthub.io/images` annotation.
Both are static, so images created at runtime are missing.

**Idea.** Compare the list a chart publishes with the images the release
actually pulled, and report the difference.

**What's new.** Evidence for a list that offline users and scanners already
rely on.

**Effort.** S.

## 9. Fix hints

**Problem.** A finding says what broke. The fix often still takes research.

**Idea.** Attach the likely fix to each finding: the line to add to the bundle
list, the chart value that sets the image registry when the chart has one, or
the setting that points a package manager at an internal mirror (for example
`PIP_INDEX_URL`).

**What's new.** Findings become actionable for people who don't know the
chart.

**Effort.** S to M.

## 10. Readiness badge

**Problem.** "Works offline" in a README is a claim without evidence.

**Idea.** A badge backed by a published report: when it ran, and with which
gapdrill version and drill file.

**What's new.** A claim you can check.

**Effort.** S, after the GitHub Action or the index (idea 3).

## 11. Drills in an existing cluster

**Problem.** Some teams can't run Docker in CI, or need to test on their own
Kubernetes distribution.

**Idea.** Run a drill in a namespace of an existing cluster, with egress
blocked by network policy and images limited to the bundle registry by an
admission policy.

**What's new.** Offline drills without a dedicated cluster.

**Effort.** L. Isolation is weaker than in a dedicated cluster (DNS, for
example, is shared), so this needs research before a design.

## 12. Fail fast during install

**Problem.** When a release's Deployment needs an image that isn't in the
bundle, `helm install --wait` waits out its whole timeout (five minutes by
default), although offline the pull will never succeed. CronJob operations
already fail 20 seconds after a pod can't pull an image.

**Idea.** Watch the release's pods while Helm installs, and stop the install
once one has failed to pull an image for 20 seconds, reporting the image.

**What's new.** A drill takes as long as its first real failure, not its
longest timeout.

**Effort.** S to M.

## 13. Unused bundle images

**Problem.** Bundles grow and rarely shrink, and every unused image costs
transfer time to sites reached over slow links or by removable media.

**Idea.** List the bundle images that nothing pulled during the drill, so
maintainers can check them and trim the bundle.

**What's new.** Bundle hygiene based on what the release actually used.

**Effort.** S. The registry log already records every pull. An image used
only by an operation the drill didn't run would also look unused, so the
report should say "not pulled during this drill", not "unused".
