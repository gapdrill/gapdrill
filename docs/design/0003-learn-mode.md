# 0003: Learn mode

- **Status:** Accepted for v0.2
- **Last updated:** 2026-11-24

## Summary

`gapdrill run --learn` runs a drill in which the nodes can pull images the
bundle lacks from their own registries, while the pods stay offline. It writes
every image the release pulled to `images.learned.txt`, marking the ones the
bundle lacked, in the format a drill's `bundle.imagesFile` reads. Maintainers
get their bundle's image list from what the release actually ran, not from
rendered templates.

## Problem

Bundle image lists are usually built by rendering the chart and collecting
image references. That misses images chosen at runtime: the ones an operator
picks, or a Job or CronJob creates. In a strict drill, each missing image
fails the operation that needs it, so a maintainer finds the missing images
one failure at a time, and an operation that fails early can hide the images
its later steps need.

## Design

A learn run changes two things; everything else stays as in a strict drill.

1. **Image pulls fall back upstream.** The containerd `hosts.toml` files still
   list the bundle registry as the first host, but have no `server` line, so
   after the bundle registry answers "not found", containerd pulls from the
   image's own registry. Because the bundle registry is still asked first, its
   log still records each image the bundle lacked.
2. **Only the pods are cut off.** The egress chain is hooked into `FORWARD`
   (pod traffic) but not `OUTPUT` (the nodes' own traffic), so containerd can
   reach registries. CoreDNS still answers NXDOMAIN for outside names, so a
   pod that needs PyPI still fails, as it would at the site.

The probe check that proves a strict drill is offline doesn't apply, since the
nodes are online by design. The pods' rules are still checked when they're
installed, and the report says the run was a learn run.

### What counts as pulled

The authoritative record is the nodes' image stores, read with
`crictl images` before the first operation and after the last. The difference
is what the release pulled. It includes images from the bundle and from
upstream, and leaves out kind's own preloaded images. Unlike the registry log,
the image store lists only pulls that succeeded, so a typo in an image name
doesn't end up in the list. If either listing fails, no list is written,
rather than a wrong one.

### Output

- **`images.learned.txt`**, one image per line, with `# new` after each image
  the bundle lacked. Image lists ignore comments, so the file works as a
  drill's `bundle.imagesFile` as it is.
- **`learnedImages`** in `report.json`, and a section in `report.md`.

Missing images still show up as findings ("…isn't in the bundle"), but they no
longer fail their operation, since the image arrives anyway.

## Alternatives considered

- **A pull-through cache in the bundle registry.** The registry would record
  and serve upstream images itself. But CNCF Distribution proxies one upstream
  registry per instance, so one registry per upstream would be needed, and the
  node would still need network access to reach them.
- **Iterating strict drills.** Re-running after adding each missing image
  works, but takes one drill per missing image and still only sees the images
  of operations that got far enough to need them.
- **Rendering the chart.** That's what most bundle tools already do, and it's
  the gap this closes.

## Security considerations

In a learn run the nodes pull from public registries with the host's network.
Only images are fetched; the pods remain offline. The bundle copy step's
credentials never reach the nodes, so the nodes pull anonymously. Pods using
`hostNetwork` share the node's network and are not cut off in a learn run.

## Future work

Writing the list as a Zarf package component or a Hauler manifest, and
upgrade drills that learn each version's list.
