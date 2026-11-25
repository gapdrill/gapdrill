// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package lockdown

import (
	"context"
	"fmt"
	"strings"
)

// Execer runs a command on a node and returns its standard output.
type Execer interface {
	Exec(ctx context.Context, node string, args ...string) (string, error)
}

// Apply installs the rules for every family on node and checks that they are
// hooked in. If ip6tables can't run and the node has no global IPv6 address,
// IPv6 is skipped (there is no IPv6 path to lock) and a warning is returned.
func Apply(ctx context.Context, x Execer, node string, n Network) (warnings []string, err error) {
	for _, f := range Families {
		if err := applyFamily(ctx, x, node, f, n); err != nil {
			if f.is4 {
				return warnings, err
			}
			out, addrErr := x.Exec(ctx, node, "ip", "-6", "addr", "show", "scope", "global")
			if addrErr != nil || strings.TrimSpace(out) != "" {
				return warnings, err
			}
			warnings = append(warnings, fmt.Sprintf("%s: ip6tables unavailable and no global IPv6 address; skipped IPv6 rules", node))
		}
	}
	return warnings, nil
}

func applyFamily(ctx context.Context, x Execer, node string, f Family, n Network) error {
	for _, c := range Rules(f, n) {
		if _, err := x.Exec(ctx, node, c...); err != nil {
			return fmt.Errorf("%s on node %s: %w", c, node, err)
		}
	}
	for _, c := range Checks(f) {
		if _, err := x.Exec(ctx, node, c...); err != nil {
			return fmt.Errorf("egress rules missing after install (%s on node %s): %w", c, node, err)
		}
	}
	return nil
}

// Rejected returns the number of packets the rules rejected on node, across
// families. Families whose chain can't be listed count as zero.
func Rejected(ctx context.Context, x Execer, node string) int64 {
	var total int64
	for _, f := range Families {
		out, err := x.Exec(ctx, node, CountCommand(f)...)
		if err != nil {
			continue
		}
		if n, err := RejectedPackets(out); err == nil {
			total += n
		}
	}
	return total
}

// Reachable reports whether node can fetch url, using curl with a short
// timeout. It is used before isolation (expected true when the host is
// online) and after (must be false).
func Reachable(ctx context.Context, x Execer, node, url string) bool {
	_, err := x.Exec(ctx, node, "curl", "--silent", "--show-error", "--output", "/dev/null", "--max-time", "5", url)
	return err == nil
}
