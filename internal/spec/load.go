// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

// Load reads a drill file, decodes it strictly (unknown fields are errors),
// applies defaults and validates it.
func Load(path string) (*Drill, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := Parse(b)
	if err != nil {
		if msg := err.Error(); strings.Contains(msg, "\n") {
			// Several problems: one per line, indented under the file name.
			return nil, fmt.Errorf("%s:\n  %s", path, strings.ReplaceAll(msg, "\n", "\n  "))
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	d.Dir = filepath.Dir(abs)
	return d, nil
}

// Parse decodes and validates a drill from YAML. Relative paths stay
// relative until Dir is set.
func Parse(b []byte) (*Drill, error) {
	var d Drill
	if err := yaml.UnmarshalStrict(b, &d); err != nil {
		return nil, err
	}
	d.applyDefaults()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

func (d *Drill) applyDefaults() {
	if d.Release.Namespace == "" {
		d.Release.Namespace = d.Release.Name
	}
	for i := range d.Operations {
		if op := d.Operations[i].RunCronJob; op != nil && op.Namespace == "" {
			op.Namespace = d.Release.Namespace
		}
	}
}

// Path resolves p against the drill file's directory.
func (d *Drill) Path(p string) string {
	if filepath.IsAbs(p) || d.Dir == "" {
		return p
	}
	return filepath.Join(d.Dir, p)
}

// ChartRef returns the chart argument to pass to Helm. A chart that exists on
// disk relative to the drill file is resolved to that path; anything else
// (oci://…, repo/chart) is passed through unchanged.
func (d *Drill) ChartRef() string {
	c := d.Release.Chart
	if strings.Contains(c, "://") {
		return c
	}
	if p := d.Path(c); exists(p) {
		return p
	}
	return c
}

// ValuesFiles returns the release's values files, resolved against the drill
// file's directory.
func (d *Drill) ValuesFiles() []string {
	out := make([]string, len(d.Release.ValuesFiles))
	for i, f := range d.Release.ValuesFiles {
		out[i] = d.Path(f)
	}
	return out
}

// BundleImages returns the bundle's image references: those in ImagesFile,
// then the inline ones, without duplicates, in order.
func (d *Drill) BundleImages() ([]string, error) {
	var refs []string
	if d.Bundle.ImagesFile != "" {
		b, err := os.ReadFile(d.Path(d.Bundle.ImagesFile))
		if err != nil {
			return nil, fmt.Errorf("bundle.imagesFile: %w", err)
		}
		lines, err := parseImageList(b)
		if err != nil {
			return nil, fmt.Errorf("bundle.imagesFile %s: %w", d.Bundle.ImagesFile, err)
		}
		refs = append(refs, lines...)
	}
	refs = append(refs, d.Bundle.Images...)

	seen := make(map[string]bool, len(refs))
	out := refs[:0]
	for _, r := range refs {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("bundle: no images listed")
	}
	return out, nil
}

func parseImageList(b []byte) ([]string, error) {
	var refs []string
	s := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; s.Scan(); n++ {
		line := s.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, " \t") {
			return nil, fmt.Errorf("line %d: expected one image reference, got %q", n, line)
		}
		refs = append(refs, line)
	}
	return refs, s.Err()
}

var (
	dnsLabel     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	dnsSubdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
)

func isDNSLabel(s string) bool     { return len(s) <= 63 && dnsLabel.MatchString(s) }
func isDNSSubdomain(s string) bool { return len(s) <= 253 && dnsSubdomain.MatchString(s) }

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
