// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// findingID identifies a finding across runs: its kind, what it's about (the
// image, the host, or the container and its exit code) and, for lookups and
// container failures, the workload behind it. An image finding leaves the
// workload out: the fix is the same whoever asked, and a run doesn't always
// see who did. Pod names, times and counts are left out, since they change
// from run to run.
func findingID(f Finding) string {
	var subject string
	owner := ""
	if f.Pod != nil {
		owner = f.Pod.Owner
	}
	switch f.Kind {
	case MissingImage, ImagePullFailed:
		subject, owner = f.Image, ""
	case NetworkLookup:
		subject = f.Host
	case ContainerFailed:
		subject = fmt.Sprintf("%s/%d", f.Container, f.ExitCode)
	default:
		subject = f.Message
	}
	sum := sha256.Sum256([]byte(string(f.Kind) + "\x00" + subject + "\x00" + owner))
	return hex.EncodeToString(sum[:8])
}

// Comparison is what changed between two runs of a drill.
type Comparison struct {
	// New findings are in the head run and not the base run.
	New []Finding
	// Fixed findings are in the base run and not the head run.
	Fixed []Finding
	// Unchanged counts the findings in both.
	Unchanged int
}

// Compare matches the findings of two reports by ID.
func Compare(base, head *Report) Comparison {
	b, h := findingsByID(base), findingsByID(head)
	var c Comparison
	for id, f := range h {
		if _, ok := b[id]; ok {
			c.Unchanged++
		} else {
			c.New = append(c.New, f)
		}
	}
	for id, f := range b {
		if _, ok := h[id]; !ok {
			c.Fixed = append(c.Fixed, f)
		}
	}
	sortByPrecedence(c.New)
	sortByPrecedence(c.Fixed)
	return c
}

func findingsByID(r *Report) map[string]Finding {
	out := map[string]Finding{}
	add := func(f Finding) {
		id := f.ID
		if id == "" { // reports from before IDs existed
			id = findingID(f)
		}
		if _, ok := out[id]; !ok {
			out[id] = f
		}
	}
	for _, op := range r.Operations {
		for _, f := range op.Findings {
			add(f)
		}
	}
	for _, f := range r.Unattributed {
		add(f)
	}
	return out
}

func sortByPrecedence(fs []Finding) {
	sort.Slice(fs, func(i, j int) bool {
		if precedence[fs[i].Kind] != precedence[fs[j].Kind] {
			return precedence[fs[i].Kind] < precedence[fs[j].Kind]
		}
		return fs[i].Message < fs[j].Message
	})
}

// Markdown renders the comparison for a pull request comment.
func (c Comparison) Markdown(drill string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## gapdrill: %s\n\n", drill)
	rest := fmt.Sprintf("%d fixed, %d unchanged.", len(c.Fixed), c.Unchanged)
	switch n := len(c.New); n {
	case 0:
		fmt.Fprintf(&b, "**✓ No new offline problems** since the base run. %s\n\n", rest)
	case 1:
		fmt.Fprintf(&b, "**✗ 1 new offline problem** since the base run. %s\n\n", rest)
	default:
		fmt.Fprintf(&b, "**✗ %d new offline problems** since the base run. %s\n\n", n, rest)
	}
	list := func(title string, fs []Finding, withFix bool) {
		if len(fs) == 0 {
			return
		}
		fmt.Fprintf(&b, "### %s\n\n", title)
		for _, f := range fs {
			fmt.Fprintf(&b, "- %s\n", f.Message)
			if withFix && f.Fix != "" {
				fmt.Fprintf(&b, "  - **Fix:** %s\n", f.Fix)
			}
		}
		b.WriteString("\n")
	}
	list("New", c.New, true)
	list("Fixed", c.Fixed, false)
	return b.String()
}
