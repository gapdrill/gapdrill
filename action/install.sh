#!/usr/bin/env bash
# Copyright The gapdrill Authors
# SPDX-License-Identifier: Apache-2.0
#
# Installs gapdrill for the GitHub Action: the release named by $VERSION,
# verified with cosign and its checksum, or a build from the action's source
# when $VERSION is empty.
set -euo pipefail

bin_dir=$RUNNER_TEMP/gapdrill/bin
mkdir -p "$bin_dir"

if [[ -n ${VERSION:-} ]]; then
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *)
    echo "::error::There is no gapdrill release for $(uname -m)"
    exit 1
    ;;
  esac

  archive=gapdrill_${VERSION#v}_${os}_${arch}.tar.gz
  url=https://github.com/gapdrill/gapdrill/releases/download/$VERSION
  download=$RUNNER_TEMP/gapdrill/download
  mkdir -p "$download"
  cd "$download"
  for file in "$archive" checksums.txt checksums.txt.sigstore.json; do
    curl -fsSL --retry 3 -o "$file" "$url/$file"
  done

  # The signature proves checksums.txt came from gapdrill's release workflow
  # for this tag; the checksum proves the archive matches it.
  cosign verify-blob \
    --bundle checksums.txt.sigstore.json \
    --certificate-identity "https://github.com/gapdrill/gapdrill/.github/workflows/release.yml@refs/tags/$VERSION" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    checksums.txt
  if command -v sha256sum >/dev/null; then
    sha256sum --check --ignore-missing checksums.txt
  else
    shasum -a 256 --check --ignore-missing checksums.txt
  fi
  tar -xzf "$archive" -C "$bin_dir" gapdrill
else
  if ! command -v go >/dev/null; then
    echo "::error::Building gapdrill from source needs Go. Use the action at a release tag, set its version input, or add actions/setup-go."
    exit 1
  fi
  (cd "$GITHUB_ACTION_PATH" && go build -trimpath -o "$bin_dir/gapdrill" ./cmd/gapdrill)
fi

echo "$bin_dir" >>"$GITHUB_PATH"
"$bin_dir/gapdrill" version
