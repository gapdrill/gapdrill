// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package version reports the gapdrill build version.
package version

import "runtime/debug"

// Version is set at build time:
//
//	-ldflags "-X github.com/gapdrill/gapdrill/internal/version.Version=v0.1.0"
var Version = ""

// String returns the build version. Without ldflags it falls back to the
// module version recorded by `go install`, then to "dev".
func String() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
