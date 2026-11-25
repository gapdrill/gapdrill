// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package cli implements the gapdrill command line.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/gapdrill/gapdrill/internal/runner"
	"github.com/gapdrill/gapdrill/internal/version"
)

// exitError carries a process exit code through cobra without printing
// anything more; the runner has already reported the outcome.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// Execute runs the command line and returns the process exit code.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := newRoot(stdout, stderr)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	var ee exitError
	switch {
	case err == nil:
		return runner.ExitPassed
	case errors.As(err, &ee):
		return ee.code
	default:
		fmt.Fprintln(stderr, "gapdrill:", err)
		return runner.ExitUsage
	}
}

func newRoot(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "gapdrill",
		Short: "Test that a Helm release works without internet access",
		Long: `gapdrill runs a Helm release in a local kind cluster that has no internet
access and can pull images only from the release's offline bundle. It then runs
the operations users depend on and reports which ones fail, and why.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRun(stdout, stderr), newValidate(stdout), newCleanup(stdout), newVersion(stdout))
	return root
}

func newRun(stdout, stderr io.Writer) *cobra.Command {
	o := runner.Options{Stdout: stdout, Stderr: stderr}
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a drill",
		Long: `Run a drill: create an offline cluster, run the drill's operations and
write report.md and report.json.

Exit codes: 0 every operation passed; 1 an operation failed; 2 the drill
couldn't run (setup, isolation or verification failed); 3 invalid input.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if code := runner.Run(cmd.Context(), o); code != runner.ExitPassed {
				return exitError{code}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.DrillPath, "file", "f", "gapdrill.yaml", "drill file")
	f.StringVar(&o.ReportDir, "report-dir", "gapdrill-report", "directory for report.md and report.json")
	f.BoolVar(&o.Keep, "keep", false, "keep the cluster afterwards for debugging (remove it with `gapdrill cleanup`)")
	f.DurationVar(&o.Timeout, "timeout", 30*time.Minute, "limit for the whole drill")
	f.StringVar(&o.ProbeURL, "probe-url", runner.DefaultProbeURL, "URL that must be reachable before isolation and unreachable after")
	f.BoolVarP(&o.Verbose, "verbose", "v", false, "print details of each phase")
	return cmd
}

func newValidate(stdout io.Writer) *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check a drill file without running it",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			d, images, err := runner.Validate(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%s: drill %q is valid: %d operations, %d bundle images\n", path, d.Metadata.Name, len(d.Operations), len(images))
			return nil
		},
	}
	cmd.Flags().StringVarP(&path, "file", "f", "gapdrill.yaml", "drill file")
	return cmd
}

func newCleanup(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Remove clusters, registries and networks left by earlier runs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runner.Cleanup(cmd.Context(), stdout)
		},
	}
}

func newVersion(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the gapdrill version",
		Args:  cobra.NoArgs,
		Run: func(*cobra.Command, []string) {
			fmt.Fprintln(stdout, version.String())
		},
	}
}
