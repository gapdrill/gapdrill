// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Command gapdrill tests that a Helm release works without internet access.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gapdrill/gapdrill/internal/cli"
)

func main() {
	// Ctrl-C cancels the drill; the runner still cleans up before exiting.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
