# The gapdrill GitHub Action

Read this when you're adding gapdrill to a GitHub Actions workflow, testing a chart on
pull requests, or working out why a gapdrill job failed.

At a release tag the action runs these steps.

1. Downloads the gapdrill release it's referenced at.
2. Verifies the download's signature with cosign, and its checksum.
3. Runs `gapdrill run`.
4. Adds the report to the job summary.
5. Uploads the report directory as an artifact.

The v0.1.0 Action exits with the drill's status. A failed operation, an
invalid input or a harness error fails the step.

## A basic job

```yaml
name: offline
on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

jobs:
  offline:
    runs-on: ubuntu-24.04  # GitHub's macOS runners have no Docker
    timeout-minutes: 45
    steps:
      - uses: actions/checkout@v7
        with:
          persist-credentials: false
      - uses: azure/setup-helm@v5
        with:
          version: v3.22.1
      - uses: gapdrill/gapdrill@v0.1.0
        with:
          drill: gapdrill.yaml
```

Check the repository's existing workflows for how it pins actions (tags or
commit SHAs), and follow the same convention.

## Inputs

| Input | Default | Meaning |
|---|---|---|
| `drill` | `gapdrill.yaml` | Path to the drill file |
| `report-dir` | `gapdrill-report` | Where the report is written |
| `args` | | Extra `gapdrill run` arguments, such as `--timeout 45m`. They're split on spaces, and quotes aren't interpreted. |
| `version` | the action's release | The gapdrill release to run, such as `v0.1.0` |
| `upload-report` | `true` | Upload the report directory as an artifact |
| `artifact-name` | `gapdrill-report` | The artifact's name. Give each drill its own. |

## Outputs

| Output | Value |
|---|---|
| `verdict` | `passed`, `failed`, `error` (the drill couldn't run) or `invalid` (bad input) |
| `exit-code` | The exit code of `gapdrill run` |
| `report` | The path to `report.json` |

To act on the outputs in a later step, give the action an `id`, add
`continue-on-error: true`, and read `steps.<id>.outputs.verdict`.

## Comparing pull requests

Use the basic job above to gate on the strict drill's verdict. The
`gapdrill compare` CLI on the v0.2 development line can also compare the
artifacts from two runs. Download the same drill's report from a trusted
base-branch run and from the pull request run into separate directories.

```sh
gh run download "$BASE_RUN" --repo "$REPO" --name gapdrill-report --dir base-report
gh run download "$HEAD_RUN" --repo "$REPO" --name gapdrill-report --dir head-report
gapdrill compare base-report/report.json head-report/report.json
```

This prints a Markdown diff. A comparison can exit 0 while the head drill
has failed operations. Inspect both the diff and the head verdict.

The proposed built-in gate introduces `compare`, `comment` and `fail-on`
inputs and a `new-findings` output. The v0.1.0 Action has six inputs and
three outputs, listed above. Before configuring the gate on another ref,
check its root `action.yml` for those fields. Selecting a different CLI
through `version` only replaces the binary.

## More than one drill in a job

Give each step its own `report-dir` and `artifact-name`:

```yaml
      - uses: gapdrill/gapdrill@v0.1.0
        with:
          drill: drills/minimal.yaml
          report-dir: minimal-report
          artifact-name: gapdrill-minimal
      - uses: gapdrill/gapdrill@v0.1.0
        with:
          drill: drills/full.yaml
          report-dir: full-report
          artifact-name: gapdrill-full
```

## Choosing the ref

| `uses:` | What runs |
|---|---|
| `gapdrill/gapdrill@v0.1.0` | That release, after its signature and checksum are verified |
| `gapdrill/gapdrill@<commit SHA>` | A release at that commit, preferring a final tag. An untagged commit builds from source and needs Go 1.26 or later. |
| A branch, or `./` in gapdrill's own repo | gapdrill built from source. Add `actions/setup-go` before it. |

## Investigating a failed job

1. Read the job summary when a report was written. For input and setup
   errors, read the failing step's log.
2. For the full report:

   ```sh
   gh run download RUN_ID --repo OWNER/REPO --name gapdrill-report --dir gapdrill-report
   ```

3. Read it as in SKILL.md, under "Read the result".
4. Check the step's `verdict` output first. `error` or `invalid` means the
   harness or its input failed, not the release.
