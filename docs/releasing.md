# Releasing

A release is cut by pushing a version tag on `main`. The
[release workflow](../.github/workflows/release.yml) builds the binaries,
generates SBOMs, signs the checksums and publishes the GitHub release. Then it
downloads the release, verifies it the way a user would and runs the binary.
CI builds the same release in snapshot mode on every pull request, so a broken
release configuration fails before it reaches a tag.

## Cut a release

1. Check that `main` is green. The release notes are generated from the pull
   requests merged since the last release, so their titles should read well.
2. Tag and push:

   ```sh
   git switch main && git pull --ff-only
   git tag -a v0.2.0 -m "v0.2.0"
   git push origin v0.2.0
   ```

   A tag like `v0.2.0-rc.1` is published as a pre-release.
3. Watch the [release workflow](https://github.com/gapdrill/gapdrill/actions/workflows/release.yml).
   Both jobs, `release` and `verify`, must pass.

## What a release contains

| File | What it is |
|---|---|
| `gapdrill_<version>_<os>_<arch>.tar.gz` | The binary, with LICENSE and README, for Linux and macOS on amd64 and arm64 |
| `*.sbom.json` | An SPDX SBOM for each archive |
| `checksums.txt` | SHA-256 checksums of the release files |
| `checksums.txt.sigstore.json` | A Sigstore bundle that signs `checksums.txt` |

Releases are signed keylessly: the signing certificate is issued to the
release workflow for that tag, so there's no long-lived key to protect.

## Verify a release

With [cosign](https://docs.sigstore.dev/cosign/system_config/installation/)
installed, in a directory with the files you downloaded:

```sh
TAG=v0.2.0
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/gapdrill/gapdrill/.github/workflows/release.yml@refs/tags/${TAG}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --check --ignore-missing checksums.txt   # macOS: shasum -a 256 --check --ignore-missing checksums.txt
```

The first command proves `checksums.txt` came from gapdrill's release workflow
for that tag. The second proves your files match it.

## Versions

gapdrill uses semantic versioning. Before v1, a minor release may change the
drill or report formats; the release notes call out any such change.
