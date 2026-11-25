# 0002: Zero-config drills

- **Status:** Accepted for v0.1
- **Last updated:** 2026-11-24

## Summary

`gapdrill run --chart ./chart --images images.txt` runs a drill with no drill
file: it installs the release, runs every CronJob once and runs the chart's
tests if it has any. `gapdrill init` writes the same drill to a file to edit.
Two new drill operations make this possible, and drill files can use them
too: `runCronJobs` and `helmTest.ifPresent`.

## Problem

Writing a drill file is the main step between hearing about gapdrill and
seeing a result. The file is also only as complete as its author's memory: a
CronJob from a subchart, or one an operator creates at runtime, is easy to
leave out. Those are exactly the operations that break offline
([harvester/harvester#10007] is a CronJob created at runtime).

## Design

### Drill format (additions to `gapdrill/v1alpha1`)

- **`runCronJobs: {timeout}`** runs every CronJob in the cluster outside
  Kubernetes' own namespaces, as found when the operation starts. They run one
  at a time, in namespace and name order, and each is reported as its own
  operation: `<operation>/<CronJob>`, or `<operation>/<namespace>/<CronJob>`
  outside the release namespace. If there are none, the operation is skipped.
- **`helmTest.ifPresent: true`** skips the operation when the release's hooks
  (`helm get hooks`) include no test hook, instead of reporting a chart with
  no tests as passing them.

The runner expands both just before they run, because what they do depends on
what earlier operations created.

### Report format (addition to `gapdrill/report/v1alpha1`)

Operations can have the status `skipped`, with the reason as the summary. A
skipped operation has no evidence window, so nothing is attributed to it, and
it doesn't count in "N of M operations failed".

### Command line

- **`gapdrill run --chart REF`** with `--images FILE` and/or `--image REF`,
  plus optional `--name`, `--namespace`, `--values FILE` and `--set key=value`,
  builds the drill in memory. The release name defaults to the name in a local
  chart's `Chart.yaml`, or else the reference's last element without its
  version. The namespace defaults to the release name.
- **`gapdrill init`** takes the same flags and writes the drill with comments,
  with paths relative to the file. It won't overwrite a file without
  `--force`.

### Failing fast on images that can't be pulled

Offline, a failed image pull never recovers. A CronJob's Job now fails once
one of its pods has been unable to pull an image for 20 seconds, instead of
waiting out its timeout (three minutes by default). The finding still names the
image, from the pod's status and the registry log. Helm installs still wait for
Helm's own timeout; failing those fast is future work.

## Alternatives considered

- **Find operations by rendering the chart.** That misses CronJobs that
  controllers create at runtime, which are the ones most likely to be
  forgotten.
- **Let CronJobs fire on their schedules**, by waiting or by faking the clock.
  That's slow and fragile. A Job created from the CronJob's template runs the
  same pod spec.
- **Report charts without tests as passing.** That's simpler, but a pass that
  tested nothing is the false confidence gapdrill exists to prevent.

## Compatibility

Every change adds to the `v1alpha1` drill and report formats. Existing drill
files behave as before, except that a `runCronJob` operation stuck on an image
pull now fails sooner and with a clearer error.

## Testing

- **Unit tests:** expansion against a fake clientset (system namespaces
  skipped, order, names, timeouts), test-hook detection, release names, `init`
  output round-tripping through the loader, skipped operations in reports, and
  command-line errors.
- **End-to-end:** the demo chart with no drill file must fail on the same two
  bugs, with the CronJob found by discovery and failing fast, and pass the
  chart's tests.

[harvester/harvester#10007]: https://github.com/harvester/harvester/issues/10007
