// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gapdrill/gapdrill/internal/report"
	"github.com/gapdrill/gapdrill/internal/spec"
	"github.com/gapdrill/gapdrill/internal/version"
)

// totalSteps is the number of numbered phases a run prints.
const totalSteps = 5

// progress prints what the drill is doing. Phases go to stderr so stdout
// holds only the summary.
type progress struct {
	mu      sync.Mutex
	w       io.Writer
	verbose bool
	n       int
	started time.Time
}

func newProgress(w io.Writer, verbose bool) *progress {
	return &progress{w: w, verbose: verbose, started: time.Now()}
}

func (p *progress) printf(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, format, args...)
}

func (p *progress) header(d *spec.Drill, images int) {
	p.printf("gapdrill %s · drill %q · %d operation%s · %d bundle image%s\n",
		version.String(), d.Metadata.Name, len(d.Operations), plural(len(d.Operations)), images, plural(images))
}

func (p *progress) step(format string, args ...any) {
	p.mu.Lock()
	p.n++
	n := p.n
	p.mu.Unlock()
	p.printf("[%d/%d] %s\n", n, totalSteps, fmt.Sprintf(format, args...))
}

func (p *progress) detail(format string, args ...any) {
	if p.verbose {
		p.printf("      %s\n", fmt.Sprintf(format, args...))
	}
}

func (p *progress) debugf(format string, args ...any) { p.detail(format, args...) }

func (p *progress) warnf(format string, args ...any) {
	p.printf("      warning: %s\n", fmt.Sprintf(format, args...))
}

func (p *progress) errorf(format string, args ...any) {
	p.printf("gapdrill: %s\n", fmt.Sprintf(format, args...))
}

func (p *progress) operation(name string, err error, d time.Duration) {
	mark := "✓"
	if err != nil {
		mark = "✗"
	}
	p.printf("      %s %s (%s)\n", mark, name, d.Round(time.Second))
}

func (p *progress) kept(name, kubeconfig string) {
	p.printf("Kept cluster %s for debugging:\n  export KUBECONFIG=%s\nRemove it with: gapdrill cleanup\n", name, kubeconfig)
}

// summary prints the verdict and one line per operation to w.
func (p *progress) summary(w io.Writer, r *report.Report, paths []string) {
	var b strings.Builder
	b.WriteString("\n")
	switch r.Verdict {
	case report.Passed:
		fmt.Fprintf(&b, "✓ Passed: every operation worked offline.\n")
	case report.Failed:
		fmt.Fprintf(&b, "✗ Failed: %d of %d operations failed offline.\n", r.FailedCount(), len(r.Operations))
	default:
		fmt.Fprintf(&b, "⚠ The drill couldn't run: %s\n", r.Error)
	}
	width := 0
	for _, op := range r.Operations {
		width = max(width, len(op.Name))
	}
	for _, op := range r.Operations {
		why := op.Summary
		if why == "" {
			why = string(op.Status)
		}
		fmt.Fprintf(&b, "  %-*s  %s\n", width, op.Name, why)
	}
	if len(paths) > 0 {
		fmt.Fprintf(&b, "\nReport: %s\n", strings.Join(paths, ", "))
	}
	fmt.Fprintf(&b, "Total time: %s\n", time.Since(p.started).Round(time.Second))
	fmt.Fprint(w, b.String())
}
