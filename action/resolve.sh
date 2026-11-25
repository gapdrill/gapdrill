#!/usr/bin/env bash
# Copyright The gapdrill Authors
# SPDX-License-Identifier: Apache-2.0
#
# Works out which gapdrill release the GitHub Action runs, and writes it to
# the step's "version" output. An empty version means: build from source,
# because the action is used at a branch or from a local path.
set -euo pipefail

repo=https://github.com/gapdrill/gapdrill.git
semver='^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'

version=${INPUT_VERSION:-}
ref=${ACTION_REF:-}

# github.action_ref can be empty inside composite actions. The action's path
# ends in the ref it was checked out at, so fall back to that.
if [[ -z $ref && $GITHUB_ACTION_PATH == */_actions/gapdrill/gapdrill/* ]]; then
  ref=${GITHUB_ACTION_PATH##*/}
fi

if [[ -z $version && $ref =~ $semver ]]; then
  version=$ref
elif [[ -z $version && $ref =~ ^[0-9a-f]{40}$ ]]; then
  # Pinned to a commit: use the release tag that points at it, preferring a
  # final release over a release candidate.
  tags=$(git ls-remote --tags "$repo" |
    awk -v sha="$ref" '$1 == sha { t = $2; sub(/^refs\/tags\//, "", t); sub(/\^\{\}$/, "", t); print t }')
  version=$(grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' <<<"$tags" | sort -V | tail -n 1 || true)
  if [[ -z $version ]]; then
    version=$(sort -V <<<"$tags" | tail -n 1)
  fi
fi

if [[ -n $version && ! $version =~ $semver ]]; then
  echo "::error::The version input must look like v1.2.3, not: $version"
  exit 1
fi

echo "version=$version" >>"$GITHUB_OUTPUT"
if [[ -n $version ]]; then
  echo "Using gapdrill $version"
else
  echo "No gapdrill release for ref '${ref:-(local path)}': building from source"
fi
