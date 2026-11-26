# Report reference (`gapdrill/report/v1alpha1`)

Read this when you need to parse `report.json`, `egress.cdx.json` or
`images.learned.txt`, or explain a report in detail. Optional fields may be omitted. v0.1.0 reports predate
`id`, `isolation.learn` and `learnedImages`.

## report.json

| Field | Meaning |
|---|---|
| `schemaVersion` | `gapdrill/report/v1alpha1` |
| `drill` | The drill's `metadata.name` |
| `gapdrillVersion` | The gapdrill version that ran it |
| `startedAt`, `finishedAt` | RFC 3339 times |
| `verdict` | `passed`, `failed` (an operation failed), or `error` (the drill couldn't run) |
| `error` | Why the drill couldn't run, when `verdict` is `error` |
| `environment` | `nodeImage`, `platform` (`linux/<arch>`), `helm` |
| `bundle.images` | The bundle's images, as canonical references |
| `bundle.notPulled` | Bundle images no node pulled during the drill. Left out when the drill couldn't run or no operation ran. |
| `isolation` | `verified`, `probeURL`, `reachableBefore`, `rejectedPackets`, `warnings`, and `learn` (true in a learn run) |
| `operations[]` | One entry per operation that ran or was skipped (fields below) |
| `unattributedFindings[]` | Findings outside every operation's window |
| `learnedImages[]` | Learn runs only: `{image, inBundle}` for every image the release pulled |

### Operation fields

| Field | Meaning |
|---|---|
| `name` | The operation's name. A `runCronJobs` operation expands into one entry per CronJob, named `<name>/<cronjob>` or `<name>/<namespace>/<cronjob>` outside the release namespace. |
| `type` | `helmInstall`, `helmTest`, `runCronJob`, `runCronJobs` (only when skipped or not expanded), or `exec` |
| `status` | `passed`, `failed` or `skipped` |
| `startedAt`, `durationSeconds` | When it started, and how long it took |
| `summary` | A one-line diagnosis for a failure, a note about warnings, or the reason it was skipped |
| `error` | The step's own error, such as Helm's `Error:` line or `job ... didn't finish within 3m0s` |
| `output` | The last 40 lines the step printed |
| `findings[]` | Findings in this operation's window (fields below) |

### Finding fields

| Field | Meaning |
|---|---|
| `kind` | `missing-image`, `network-lookup`, `image-pull-failed` or `container-failed` |
| `message` | A full sentence, such as `CronJob demo-cleanup needs docker.io/library/busybox:1.37.0, which isn't in the bundle.` |
| `image` | The image, for image findings |
| `host` | The name looked up, for `network-lookup` |
| `container`, `exitCode` | For `container-failed` |
| `pod` | `{namespace, name, owner}`, when the pod is known. `owner` is readable, such as `Deployment web` or `Job demo-post-install (Helm post-install hook)`. |
| `firstSeen` | When it was first seen |
| `count` | How many times it was seen. For lookups, this is the number of queries. |
| `logs` | The tail of a failed container's log |
| `fix` | The likely fix, when there's a useful one |
| `id` | 16 hex characters that identify the finding across runs |

Each operation's findings are sorted strongest cause first: missing image,
lookup, pull failure, then container failure.

A finding's `id` hashes three things:

- its kind;
- what it's about: the image, the host, or the container and its exit code;
- the workload behind it, except for image findings.

So the same problem in two runs has the same `id`, even with new pod names.
`gapdrill compare` computes ids for older reports that lack them.

### jq recipes

```sh
R=gapdrill-report/report.json
jq -r .verdict "$R"
# Failed operations and why
jq -r '.operations[] | select(.status == "failed") | "\(.name): \(.summary)"' "$R"
# Every finding, with its subject and fix
jq -r '[.operations[].findings[]?, .unattributedFindings[]?] | .[] | [.kind, (.image // .host // .container), (.pod.owner // "-"), (.fix // "")] | @tsv' "$R"
# Images to add to the bundle
jq -r '[.operations[].findings[]?, .unattributedFindings[]?] | map(select(.kind == "missing-image") | .image) | unique[]' "$R"
# Hosts the release tried to reach
jq -r '[.operations[].findings[]?, .unattributedFindings[]?] | map(select(.kind == "network-lookup") | .host) | unique[]' "$R"
```

## report.md

The sections, in order:

1. The headline and the environment line.
2. A table with each operation's result, time and why.
3. A section for each operation that failed or has findings, listing the
   findings with their fixes, then the step's error and output.
4. "Outside any operation": the unattributed findings.
5. "Bundle images not pulled".
6. "Images the release pulled", for learn runs.
7. "How the cluster was isolated".

In GitHub Actions, gapdrill also appends `report.md` to the job summary.

## egress.cdx.json

This is a CycloneDX 1.6 BOM. Each outside host that a workload looked up
during the drill becomes one entry in `services[]`:

```json
{
  "bom-ref": "egress:pypi.org",
  "name": "pypi.org",
  "endpoints": ["dns:pypi.org"],
  "x-trust-boundary": true,
  "properties": [
    {"name": "gapdrill:workload", "value": "Job demo-post-install (Helm post-install hook)"},
    {"name": "gapdrill:lookups", "value": "2"}
  ]
}
```

Each workload adds one `gapdrill:workload` and `gapdrill:lookups` pair. The
file isn't written when the drill couldn't run: it would wrongly say the
release contacts nothing. To list the hosts, run
`jq -r '.services[].name' gapdrill-report/egress.cdx.json`.

## images.learned.txt

A learn run writes this file if at least one successfully pulled reference
appears in the final image store that was absent from the initial store.
A line ending in ` # new` marks an image the bundle lacked.

```text
# The images drill "demo" pulled during a gapdrill learn run (gapdrill dev).
# Use this as the bundle's image list. Images marked "new" weren't in the bundle.
docker.io/library/busybox:1.36.1
docker.io/library/busybox:1.37.0 # new
docker.io/library/python:3.12-alpine
```

## gapdrill compare output

The output is Markdown, ready for a pull request comment. It starts with a
`## gapdrill: <drill>` heading and one of two lines:

- `**✓ No new offline problems** since the base run. X fixed, Y unchanged.`
- `**✗ N new offline problems** since the base run. ...`

Then come a `### New` list, with each finding's fix, and a `### Fixed` list.
