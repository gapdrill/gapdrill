# Security policy

## Reporting a vulnerability

Report security problems privately through GitHub: open the repository's
**Security** tab and choose **Report a vulnerability**. Please don't open a
public issue.

Include what you found, how to reproduce it, and the gapdrill version
(`gapdrill version`). We aim to acknowledge reports within 7 days and to agree
a disclosure date with you.

## Supported versions

Before v1, only the latest release gets security fixes.

## Scope

gapdrill runs on a developer's machine or a CI runner. It starts privileged
kind node containers and a local registry, and it runs the drill file's
`exec` commands on the host. Drill files are trusted input, like Makefiles.
Problems in scope include gapdrill changing anything outside the resources it
creates, the bundle registry being reachable beyond `127.0.0.1`, and
credentials leaking into the cluster or the report.
