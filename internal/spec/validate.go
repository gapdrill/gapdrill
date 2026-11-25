// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"errors"
	"fmt"
)

// Validate reports every problem in the drill at once, each prefixed with the
// field it belongs to.
func (d *Drill) Validate() error {
	var errs []error
	add := func(field, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", field, fmt.Sprintf(format, args...)))
	}

	if d.APIVersion != APIVersion {
		add("apiVersion", "must be %q, got %q", APIVersion, d.APIVersion)
	}
	if d.Kind != Kind {
		add("kind", "must be %q, got %q", Kind, d.Kind)
	}
	if !isDNSLabel(d.Metadata.Name) {
		add("metadata.name", "must be a lowercase DNS label (a-z, 0-9 and '-', at most 63 characters), got %q", d.Metadata.Name)
	}
	if d.Bundle.ImagesFile == "" && len(d.Bundle.Images) == 0 {
		add("bundle", "set imagesFile, images, or both")
	}
	if d.Release.Chart == "" {
		add("release.chart", "is required")
	}
	if !isDNSLabel(d.Release.Name) || len(d.Release.Name) > 53 {
		add("release.name", "must be a lowercase DNS label of at most 53 characters, got %q", d.Release.Name)
	}
	if !isDNSLabel(d.Release.Namespace) {
		add("release.namespace", "must be a lowercase DNS label, got %q", d.Release.Namespace)
	}

	if len(d.Operations) == 0 {
		add("operations", "at least one operation is required")
	}
	seen := make(map[string]bool, len(d.Operations))
	installed := false
	for i, op := range d.Operations {
		f := fmt.Sprintf("operations[%d]", i)
		switch {
		case op.Name == "":
			add(f+".name", "is required")
		case seen[op.Name]:
			add(f+".name", "%q is used by an earlier operation", op.Name)
		}
		seen[op.Name] = true

		switch op.typeCount() {
		case 0:
			add(f, "set one of helmInstall, helmTest, runCronJob, runCronJobs or exec")
			continue
		case 1:
		default:
			add(f, "set only one of helmInstall, helmTest, runCronJob, runCronJobs or exec")
			continue
		}

		switch {
		case op.HelmInstall != nil:
			installed = true
		case op.HelmTest != nil:
			if !installed {
				add(f+".helmTest", "needs a helmInstall operation before it")
			}
		case op.RunCronJob != nil:
			if !isDNSSubdomain(op.RunCronJob.Name) {
				add(f+".runCronJob.name", "must be a CronJob name, got %q", op.RunCronJob.Name)
			}
			if !isDNSLabel(op.RunCronJob.Namespace) {
				add(f+".runCronJob.namespace", "must be a lowercase DNS label, got %q", op.RunCronJob.Namespace)
			}
		case op.Exec != nil:
			if len(op.Exec.Command) == 0 || op.Exec.Command[0] == "" {
				add(f+".exec.command", "is required")
			}
		}
	}
	return errors.Join(errs...)
}
