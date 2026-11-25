# Contributing to gapdrill

Thanks for helping. This page covers how to build, test and send changes.

## Development setup

You need Go 1.26 or later, Docker (Docker Desktop works) and Helm 3 or 4.

```sh
make build      # bin/gapdrill
make test       # unit tests, with the race detector
make lint       # golangci-lint
make e2e        # the demo drills on real kind clusters (about five minutes)
```

Unit tests need no Docker. Put new logic in the pure packages (`spec`,
`mirror`, `lockdown`, `dnsmon`, `report`) where it can be unit-tested, and keep
the I/O packages thin. If you change the Markdown report, regenerate its
golden file with `go test ./internal/report -update` and review the diff.

## Sending a change

1. For anything larger than a bug fix, open an issue first so we can agree on
   the approach. Changes to the drill or report format, or to how isolation
   works, need a short design doc in `docs/design/` (copy the structure of
   `0001-architecture.md`).
2. Keep pull requests focused, with tests. CI runs the unit tests, the linter
   and the end-to-end drills.
3. Sign off every commit (see below).

## Developer Certificate of Origin

gapdrill uses the [Developer Certificate of Origin](https://developercertificate.org/)
(DCO). By signing off a commit you certify that you wrote it, or otherwise
have the right to submit it under the project's license. Add the sign-off
with `git commit -s`, which appends:

```text
Signed-off-by: Your Name <you@example.com>
```

The name and email must match the commit author. To sign off commits you've
already made, run `git rebase --signoff main`.

## Reporting bugs and security issues

Open an issue with the drill file, the command you ran and `report.md`. Don't
report security problems in public issues; see [SECURITY.md](SECURITY.md).

## Code of Conduct

This project follows the [CNCF Code of Conduct](CODE_OF_CONDUCT.md).
