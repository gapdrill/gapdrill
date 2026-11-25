// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"fmt"
	"strings"
	"time"
)

// Markdown renders the report for people: a summary table, then the details
// of every operation with findings or errors.
func (r *Report) Markdown() string {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	p("# gapdrill: %s\n\n", r.Drill)
	p("%s\n\n", r.headline())
	p("gapdrill %s · %s · %s", r.GapdrillVersion, r.Environment.NodeImage, r.Environment.Platform)
	if r.Environment.Helm != "" {
		p(" · Helm %s", r.Environment.Helm)
	}
	p(" · %d bundle image%s\n\n", len(r.Bundle.Images), plural(len(r.Bundle.Images)))

	if len(r.Operations) > 0 {
		p("| Operation | Result | Time | Why |\n|---|---|---|---|\n")
		for _, op := range r.Operations {
			p("| %s | %s | %s | %s |\n", cell(op.Name), statusLabel(op.Status), seconds(op.DurationSeconds), cell(op.Summary))
		}
		p("\n")
	}

	for _, op := range r.Operations {
		if op.Status == StatusPassed && len(op.Findings) == 0 {
			continue
		}
		p("## %s: %s\n\n", op.Name, op.Status)
		if op.Summary != "" {
			p("%s\n\n", op.Summary)
		}
		writeFindings(&b, op.Findings)
		if op.Error != "" && op.Error != op.Summary {
			p("Error: `%s`\n\n", strings.ReplaceAll(op.Error, "`", "'"))
		}
		if out := strings.TrimRight(op.Output, "\n"); out != "" {
			p("<details><summary>%s output</summary>\n\n```text\n%s\n```\n\n</details>\n\n", op.Type, out)
		}
	}

	if len(r.Unattributed) > 0 {
		p("## Outside any operation\n\n")
		writeFindings(&b, r.Unattributed)
	}

	p("## How the cluster was isolated\n\n")
	p("- Nodes and pods could reach only the cluster and the bundle registry (iptables and ip6tables rules inside each node).\n")
	p("- Names outside the cluster got NXDOMAIN from CoreDNS.\n")
	p("- Every registry, including ones not in the bundle, was served by the bundle registry, with no upstream fallback.\n")
	switch {
	case r.Isolation.Verified && r.Isolation.ReachableBefore:
		p("- Verified: %s was reachable from the node before isolation and unreachable after.\n", r.Isolation.ProbeURL)
	case r.Isolation.Verified:
		p("- %s was unreachable after isolation, but also before it, so the probe proves less than usual: the host may be offline.\n", r.Isolation.ProbeURL)
	default:
		p("- Isolation was not verified.\n")
	}
	if r.Isolation.RejectedPackets > 0 {
		p("- %d outbound packet%s rejected during the drill.\n", r.Isolation.RejectedPackets, pluralWas(r.Isolation.RejectedPackets))
	}
	for _, w := range r.Isolation.Warnings {
		p("- Warning: %s\n", w)
	}
	return b.String()
}

func (r *Report) headline() string {
	n := len(r.Operations)
	switch r.Verdict {
	case Passed:
		if n == 1 {
			return "**✓ Passed:** the operation passed."
		}
		return fmt.Sprintf("**✓ Passed:** all %d operations passed.", n)
	case Failed:
		return fmt.Sprintf("**✗ Failed:** %d of %d operation%s failed.", r.FailedCount(), n, plural(n))
	default:
		return "**⚠ The drill couldn't run:** " + r.Error
	}
}

// writeFindings lists the evidence compactly: what, who, where. The full
// sentence is already the operation's summary (and is in report.json).
func writeFindings(b *strings.Builder, fs []Finding) {
	for _, f := range fs {
		var what string
		switch f.Kind {
		case NetworkLookup:
			what = fmt.Sprintf("**Blocked lookup** `%s`", f.Host)
		case MissingImage:
			what = fmt.Sprintf("**Missing image** `%s`", f.Image)
		case ImagePullFailed:
			what = fmt.Sprintf("**Pull failed** `%s`: %s", f.Image, f.Message)
		case ContainerFailed:
			what = fmt.Sprintf("**Container failed** `%s` exited with code %d", f.Container, f.ExitCode)
		default:
			what = f.Message
		}
		parts := []string{what}
		if f.Pod != nil {
			parts = append(parts, f.Pod.Owner, fmt.Sprintf("pod `%s/%s`", f.Pod.Namespace, f.Pod.Name))
		}
		if f.Kind == NetworkLookup && f.Count > 1 {
			parts = append(parts, fmt.Sprintf("%d queries", f.Count))
		}
		fmt.Fprintf(b, "- %s\n", strings.Join(parts, " · "))
		if len(f.Logs) > 0 {
			b.WriteString("\n  ```text\n")
			for _, l := range f.Logs {
				fmt.Fprintf(b, "  %s\n", l)
			}
			b.WriteString("  ```\n")
		}
	}
	b.WriteString("\n")
}

func statusLabel(s Status) string {
	if s == StatusPassed {
		return "✓ passed"
	}
	return "✗ failed"
}

// seconds formats whole seconds as "34s", "1m" or "1m5s".
func seconds(s float64) string {
	d := time.Duration(s) * time.Second
	if d < time.Minute {
		return d.String()
	}
	out := d.Truncate(time.Minute).String()
	out = strings.TrimSuffix(out, "0s")
	if rest := d % time.Minute; rest > 0 {
		out += rest.String()
	}
	return out
}

func cell(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), "|", `\|`)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pluralWas(n int64) string {
	if n == 1 {
		return " was"
	}
	return "s were"
}
