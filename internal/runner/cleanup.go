// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gapdrill/gapdrill/internal/cluster"
	"github.com/gapdrill/gapdrill/internal/docker"
)

// Cleanup removes everything gapdrill runs left behind: kind clusters named
// gapdrill-*, and containers and networks carrying the run label.
func Cleanup(ctx context.Context, out io.Writer) error {
	d, err := docker.New(ctx)
	if err != nil {
		return err
	}
	clusters, err := cluster.List(namePrefix)
	if err != nil {
		return err
	}
	// kind removes a deleted cluster's kubeconfig entry; give it a throwaway
	// path so the user's ~/.kube/config is never touched.
	tmp, err := os.MkdirTemp("", "gapdrill-cleanup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for _, name := range clusters {
		if err := cluster.DeleteByName(name, filepath.Join(tmp, "kubeconfig")); err != nil {
			return fmt.Errorf("delete cluster %s: %w", name, err)
		}
		fmt.Fprintf(out, "deleted cluster %s\n", name)
	}

	containers, err := d.Run(ctx, "ps", "--all", "--filter", "label="+runLabel, "--format", "{{.Names}}")
	if err != nil {
		return err
	}
	for _, c := range strings.Fields(containers) {
		if _, err := d.Run(ctx, "rm", "--force", c); err != nil {
			return err
		}
		fmt.Fprintf(out, "removed container %s\n", c)
	}
	networks, err := d.Run(ctx, "network", "ls", "--filter", "label="+runLabel, "--format", "{{.Name}}")
	if err != nil {
		return err
	}
	for _, n := range strings.Fields(networks) {
		if _, err := d.Run(ctx, "network", "rm", n); err != nil {
			return err
		}
		fmt.Fprintf(out, "removed network %s\n", n)
	}
	if len(clusters) == 0 && containers == "" && networks == "" {
		fmt.Fprintln(out, "nothing to clean up")
	}
	return nil
}
