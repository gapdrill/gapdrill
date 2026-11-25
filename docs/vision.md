# Vision

gapdrill exists so that nobody learns their release doesn't work offline from
a user at a disconnected site.

## The problem

Kubernetes runs in many places that can't reach the internet: defense and
government networks, factories, ships, hospitals and research labs. The
software installed there is built and tested by teams whose internet is always
on. Whether a release works offline is discovered by the user, in the field,
where a fix takes longest and costs the most.

## Where we're going

**Make "works offline" something a release can prove, explain and show.**

Today gapdrill proves it for one Helm release, on a laptop or in CI. The goal
is for an offline drill to be a normal step in releasing software meant for
disconnected sites, and for its result to be something users check before they
install.

## Three jobs

1. **Prove.** Run the release where the internet doesn't exist, with only its
   bundle, and exercise what users actually do: install, upgrade, scheduled
   jobs, tests. Verify the isolation itself, so that a pass means something.
2. **Explain and fix.** Every failure names the workload, the image or host it
   needed, and the change that fixes it. Where a drill observes the answer,
   such as the images a release really pulls, gapdrill writes it down for you.
3. **Show.** Produce results other people can trust without rerunning them:
   reports for maintainers, machine-readable records for security reviewers,
   and public evidence for users.

gapdrill does the first job today and part of the second. The
[roadmap](../ROADMAP.md) and [ideas](ideas.md) cover the rest.

## Who it's for

- **Maintainers** of charts and operators whose users run offline. They want
  to catch offline bugs before a release, not from issues after it.
- **Platform teams** that ship software to disconnected sites. They want every
  release checked the same way, in CI.
- **Security and compliance reviewers** at those sites. They want evidence of
  what a release tries to contact.

## Principles

These decide trade-offs when ideas compete.

1. **Trustworthy before clever.** Never report a pass that can't be proven.
   Isolation is verified before anything runs, and a broken drill is never
   reported as a broken release.
2. **First result in minutes.** One command and sensible defaults. A drill
   file adds detail; it shouldn't be the price of entry.
3. **Explain, don't just fail.** A finding says who needed what, and how to
   fix it.
4. **Complement the ecosystem.** Zarf, Hauler and others build and move
   bundles; chart-testing tests charts online. gapdrill tests the offline path
   and feeds what it learns back to those tools.
5. **Local first, CI native.** The same result on a laptop and in CI, with no
   service or account.
6. **Vendor neutral.** Any chart, bundle format, registry or supported
   distribution. No feature depends on one company's product.

## What success looks like

- A maintainer adds gapdrill to CI in under ten minutes, and it catches a real
  offline bug before a release.
- A report is enough to fix the problem without rerunning anything.
- People who run disconnected sites look for a gapdrill result before they
  install a chart.

Two numbers matter more than the rest: how many projects run a drill in CI,
and how many offline bugs were fixed upstream because a drill found them.

## What we won't do

- **Build bundles.** Packaging tools do that well. gapdrill tests their output
  and can suggest what belongs in it.
- **Run a hosted service.** gapdrill runs where you run it.
- **Test application logic.** A drill checks what breaks without the internet,
  not whether the application is correct. That's what a project's own tests
  are for.

## How ideas become features

1. An idea goes in [ideas.md](ideas.md) or in an issue labelled `idea`.
2. An idea that changes the drill or report formats, or how isolation works,
   gets a design doc in [design/](design/).
3. Committed work goes on the [roadmap](../ROADMAP.md) and ships in a release.
