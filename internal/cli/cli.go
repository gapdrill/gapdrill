// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package cli implements the gapdrill command line.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gapdrill/gapdrill/internal/runner"
	"github.com/gapdrill/gapdrill/internal/spec"
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
	root.AddCommand(newRun(stdout, stderr), newInit(stdout), newValidate(stdout), newCleanup(stdout), newVersion(stdout))
	return root
}

// autoFlags describe a drill built from flags instead of a drill file, for
// `run --chart` and `init`.
type autoFlags struct {
	chart, name, namespace, imagesFile string
	images, values, set                []string
}

// autoFlagNames are the flags that only mean something with --chart.
var autoFlagNames = []string{"images", "image", "name", "namespace", "values", "set"}

func (a *autoFlags) register(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&a.chart, "chart", "", "the chart to drill: a local chart directory or any reference Helm accepts")
	f.StringVar(&a.imagesFile, "images", "", "file listing the bundle's images, one per line")
	f.StringArrayVar(&a.images, "image", nil, "an image in the bundle (repeatable)")
	f.StringVar(&a.name, "name", "", "release name (default: the chart's name)")
	f.StringVar(&a.namespace, "namespace", "", "release namespace (default: the release name)")
	f.StringArrayVar(&a.values, "values", nil, "a values file (repeatable)")
	f.StringArrayVar(&a.set, "set", nil, "a chart value, as key=value (repeatable)")
}

// drill builds the drill the flags describe. Paths are relative to the
// current directory.
func (a *autoFlags) drill() (*spec.Drill, error) {
	if a.imagesFile == "" && len(a.images) == 0 {
		return nil, errors.New("--chart needs the bundle's images: --images FILE, --image REF, or both")
	}
	var set map[string]string
	for _, kv := range a.set {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--set %q: want key=value", kv)
		}
		if set == nil {
			set = map[string]string{}
		}
		set[k] = v
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	d, err := spec.Auto(spec.AutoOptions{
		Chart:       a.chart,
		Name:        a.name,
		Namespace:   a.namespace,
		ImagesFile:  a.imagesFile,
		Images:      a.images,
		ValuesFiles: a.values,
		Set:         set,
		Dir:         dir,
	})
	if err != nil {
		return nil, fmt.Errorf("can't build a drill from the flags: %w", err)
	}
	return d, nil
}

func newRun(stdout, stderr io.Writer) *cobra.Command {
	o := runner.Options{Stdout: stdout, Stderr: stderr}
	var auto autoFlags
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a drill",
		Long: `Run a drill: create an offline cluster, run the drill's operations and
write report.md and report.json.

The drill comes from a drill file (--file), or from flags when --chart is set:
install the chart, run every CronJob once, and run the chart's tests if it has
any.

Exit codes: 0 every operation passed; 1 an operation failed; 2 the drill
couldn't run (setup, isolation or verification failed); 3 invalid input.`,
		Example: `  gapdrill run                                          # reads ./gapdrill.yaml
  gapdrill run --chart ./charts/web --images images.txt`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if auto.chart != "" {
				if cmd.Flags().Changed("file") {
					return errors.New("use --file or --chart, not both")
				}
				d, err := auto.drill()
				if err != nil {
					return err
				}
				o.Drill = d
			} else {
				for _, name := range autoFlagNames {
					if cmd.Flags().Changed(name) {
						return fmt.Errorf("--%s needs --chart", name)
					}
				}
			}
			if code := runner.Run(cmd.Context(), o); code != runner.ExitPassed {
				return exitError{code}
			}
			return nil
		},
	}
	auto.register(cmd)
	f := cmd.Flags()
	f.StringVarP(&o.DrillPath, "file", "f", "gapdrill.yaml", "drill file")
	f.StringVar(&o.ReportDir, "report-dir", "gapdrill-report", "directory for report.md and report.json")
	f.BoolVar(&o.Keep, "keep", false, "keep the cluster afterwards for debugging (remove it with `gapdrill cleanup`)")
	f.DurationVar(&o.Timeout, "timeout", 30*time.Minute, "limit for the whole drill")
	f.StringVar(&o.ProbeURL, "probe-url", runner.DefaultProbeURL, "URL that must be reachable before isolation and unreachable after")
	f.BoolVarP(&o.Verbose, "verbose", "v", false, "print details of each phase")
	f.BoolVar(&o.Learn, "learn", false, "let the nodes pull images the bundle lacks from their own registries (pods stay offline), and write every image the release pulled to images.learned.txt")
	return cmd
}

func newInit(stdout io.Writer) *cobra.Command {
	var a autoFlags
	var out string
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a drill file for a chart",
		Long: `Write a drill file that installs the chart, runs every CronJob once and runs
the chart's tests if it has any: the drill "gapdrill run --chart" runs. Edit it
to add the operations your users depend on.`,
		Example: `  gapdrill init --chart ./charts/web --images images.txt
  gapdrill run`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if a.chart == "" {
				return errors.New("--chart is required")
			}
			d, err := a.drill()
			if err != nil {
				return err
			}
			abs, err := filepath.Abs(out)
			if err != nil {
				return err
			}
			b, err := spec.InitFile(d, filepath.Dir(abs))
			if err != nil {
				return err
			}
			flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
			if !force {
				flags |= os.O_EXCL
			}
			f, err := os.OpenFile(out, flags, 0o644)
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("%s already exists (use --force to overwrite it)", out)
			}
			if err != nil {
				return err
			}
			_, err = f.Write(b)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
			if _, err := spec.Load(out); err != nil {
				return fmt.Errorf("wrote %s, but it doesn't load (this is a gapdrill bug): %w", out, err)
			}
			next := "gapdrill run"
			if out != "gapdrill.yaml" {
				next += " -f " + out
			}
			fmt.Fprintf(stdout, "Wrote %s. Add the steps your users depend on, then run: %s\n", out, next)
			return nil
		},
	}
	a.register(cmd)
	cmd.Flags().StringVarP(&out, "output", "o", "gapdrill.yaml", "file to write")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file if it exists")
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
