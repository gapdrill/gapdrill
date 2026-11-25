// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"sigs.k8s.io/yaml"
)

// AutoOptions describe a release for a drill built from command-line flags
// instead of a drill file.
type AutoOptions struct {
	// Chart is a local chart directory or any chart reference Helm accepts.
	Chart string
	// Name defaults to the chart's name, and Namespace to Name.
	Name, Namespace string
	ImagesFile      string
	Images          []string
	ValuesFiles     []string
	Set             map[string]string
	// Dir is the directory relative paths resolve against.
	Dir string
}

// AutoOperations are what a drill built from flags exercises: the install,
// every CronJob once, and the chart's tests if it has any.
func AutoOperations() []Operation {
	return []Operation{
		{Name: "install", HelmInstall: &HelmInstallOp{}},
		{Name: "cronjobs", RunCronJobs: &RunCronJobsOp{}},
		{Name: "chart-tests", HelmTest: &HelmTestOp{IfPresent: true}},
	}
}

// Auto builds and validates the drill for o.
func Auto(o AutoOptions) (*Drill, error) {
	name := o.Name
	if name == "" {
		name = ReleaseNameFor(o.Chart, o.Dir)
	}
	d := &Drill{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: name},
		Bundle:     Bundle{ImagesFile: o.ImagesFile, Images: o.Images},
		Release: Release{
			Chart:       o.Chart,
			Name:        name,
			Namespace:   o.Namespace,
			ValuesFiles: o.ValuesFiles,
			Set:         o.Set,
		},
		Operations: AutoOperations(),
		Dir:        o.Dir,
	}
	d.applyDefaults()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
}

var (
	chartVersionSuffix = regexp.MustCompile(`-v?[0-9]+\.[0-9]+\.[0-9]+.*$`)
	notDNSLabelChars   = regexp.MustCompile(`[^a-z0-9-]+`)
)

// ReleaseNameFor derives a release name from a chart reference: the name in a
// local chart's Chart.yaml, or else the reference's last element without a
// version, as a DNS label of at most 53 characters.
func ReleaseNameFor(chart, dir string) string {
	base := chart
	if !strings.Contains(chart, "://") {
		p := chart
		if !filepath.IsAbs(p) && dir != "" {
			p = filepath.Join(dir, p)
		}
		if abs, err := filepath.Abs(p); err == nil && exists(abs) {
			if name := chartYAMLName(abs); name != "" {
				base = name
			} else {
				base = abs
			}
		}
	}
	base = strings.TrimRight(base, "/")
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base, _, _ = strings.Cut(base, ":") // oci://…/chart:1.2.3
	base = strings.TrimSuffix(base, ".tgz")
	base = chartVersionSuffix.ReplaceAllString(base, "")
	name := strings.Trim(notDNSLabelChars.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if len(name) > 53 {
		name = strings.TrimRight(name[:53], "-")
	}
	if name == "" {
		return "release"
	}
	return name
}

func chartYAMLName(chartDir string) string {
	b, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml"))
	if err != nil {
		return ""
	}
	var c struct {
		Name string `json:"name"`
	}
	if yaml.Unmarshal(b, &c) != nil {
		return ""
	}
	return c.Name
}

// InitFile renders d, a drill from Auto, as a commented drill file to be
// written to dir. Local paths are made relative to dir.
func InitFile(d *Drill, dir string) ([]byte, error) {
	f := *d
	f.Release.Chart = relPath(d, dir, d.Release.Chart, true)
	f.Bundle.ImagesFile = relPath(d, dir, d.Bundle.ImagesFile, false)
	f.Release.ValuesFiles = make([]string, len(d.Release.ValuesFiles))
	for i, v := range d.Release.ValuesFiles {
		f.Release.ValuesFiles[i] = relPath(d, dir, v, false)
	}
	var b bytes.Buffer
	if err := initTemplate.Execute(&b, &f); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// relPath rewrites p, relative to the drill's directory, as a path relative
// to dir. References that aren't files on disk are returned unchanged.
func relPath(d *Drill, dir, p string, chart bool) string {
	if p == "" || strings.Contains(p, "://") {
		return p
	}
	abs, err := filepath.Abs(d.Path(p))
	if err != nil || !exists(abs) {
		return p
	}
	rel, err := filepath.Rel(dir, abs)
	if err != nil {
		return abs
	}
	// A bare name such as "mychart" could be read as a repo/chart reference.
	if chart && !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return filepath.ToSlash(rel)
}

// quote writes s as a YAML double-quoted string (JSON strings are valid YAML).
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

var initTemplate = template.Must(template.New("drill").Funcs(template.FuncMap{"q": quote}).Parse(
	`# A gapdrill drill. Reference: https://github.com/gapdrill/gapdrill#drill-file-reference
apiVersion: {{ .APIVersion }}
kind: {{ .Kind }}
metadata:
  name: {{ .Metadata.Name }}
bundle:
{{- if .Bundle.ImagesFile }}
  imagesFile: {{ q .Bundle.ImagesFile }} # the images the offline bundle ships, one per line
{{- end }}
{{- if .Bundle.Images }}
  images:
{{- range .Bundle.Images }}
    - {{ q . }}
{{- end }}
{{- end }}
release:
  chart: {{ q .Release.Chart }}
  name: {{ .Release.Name }}
  namespace: {{ .Release.Namespace }}
{{- if .Release.ValuesFiles }}
  valuesFiles:
{{- range .Release.ValuesFiles }}
    - {{ q . }}
{{- end }}
{{- end }}
{{- if .Release.Set }}
  set:
{{- range $k, $v := .Release.Set }}
    {{ q $k }}: {{ q $v }}
{{- end }}
{{- end }}
operations:
  - name: install
    helmInstall: {}
  # Every CronJob in the cluster, run once. That includes CronJobs the
  # release creates at runtime.
  - name: cronjobs
    runCronJobs: {}
  # The chart's tests, skipped if the chart has none.
  - name: chart-tests
    helmTest:
      ifPresent: true
  # Add the steps your users depend on, for example:
  # - name: backup
  #   exec:
  #     command: ["./scripts/backup.sh"]
`))
