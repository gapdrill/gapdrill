// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReleaseNameFor(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "charts", "web", "Chart.yaml"), "apiVersion: v2\nname: storefront\nversion: 1.0.0\n")
	if err := os.MkdirAll(filepath.Join(dir, "No_Chart_Yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ chart, want string }{
		{"./charts/web", "storefront"}, // the name in Chart.yaml beats the directory name
		{"No_Chart_Yaml", "no-chart-yaml"},
		{"oci://ghcr.io/acme/charts/demo", "demo"},
		{"oci://ghcr.io/acme/charts/demo:1.2.3", "demo"},
		{"bitnami/nginx", "nginx"},
		{"nginx-15.4.2.tgz", "nginx"},
		{"https://example.com/charts/redis-v7.0.1.tgz", "redis"},
		{strings.Repeat("a", 60), strings.Repeat("a", 53)},
		{"___", "release"},
	} {
		if got := ReleaseNameFor(tt.chart, dir); got != tt.want {
			t.Errorf("ReleaseNameFor(%q) = %q, want %q", tt.chart, got, tt.want)
		}
	}
}

func TestAutoBuildsAValidDrill(t *testing.T) {
	d, err := Auto(AutoOptions{Chart: "oci://ghcr.io/acme/charts/demo", Images: []string{"busybox:1.36.1"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Release.Name != "demo" || d.Release.Namespace != "demo" || d.Metadata.Name != "demo" {
		t.Errorf("names: release %q, namespace %q, drill %q", d.Release.Name, d.Release.Namespace, d.Metadata.Name)
	}
	var types []string
	for _, op := range d.Operations {
		types = append(types, op.Type())
	}
	if want := []string{TypeHelmInstall, TypeRunCronJobs, TypeHelmTest}; !reflect.DeepEqual(types, want) {
		t.Errorf("operations = %v, want %v", types, want)
	}
	if !d.Operations[2].HelmTest.IfPresent {
		t.Error("the tests operation must skip charts without tests")
	}

	if _, err := Auto(AutoOptions{Chart: "demo"}); err == nil || !strings.Contains(err.Error(), "bundle") {
		t.Errorf("a drill without bundle images must be rejected, got %v", err)
	}
}

func TestInitFileRoundTrips(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "charts", "web", "Chart.yaml"), "apiVersion: v2\nname: web\nversion: 1.0.0\n")
	write(t, filepath.Join(root, "bundle", "images.txt"), "busybox:1.36.1\n")
	write(t, filepath.Join(root, "values-airgap.yaml"), "registry: registry.local\n")

	d, err := Auto(AutoOptions{
		Chart:       "charts/web",
		ImagesFile:  "bundle/images.txt",
		Images:      []string{"docker.io/library/alpine:3.20"},
		ValuesFiles: []string{"values-airgap.yaml"},
		Set:         map[string]string{"image.pullPolicy": "IfNotPresent", "replicas": "1"},
		Namespace:   "web-system",
		Dir:         root,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Written to a subdirectory, so every local path must be rewritten.
	outDir := filepath.Join(root, "drills")
	b, err := InitFile(d, outDir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(outDir, "gapdrill.yaml"), string(b))
	got, err := Load(filepath.Join(outDir, "gapdrill.yaml"))
	if err != nil {
		t.Fatalf("the generated file doesn't load: %v\n%s", err, b)
	}

	if got.ChartRef() != filepath.Join(root, "charts", "web") {
		t.Errorf("chart resolves to %q", got.ChartRef())
	}
	images, err := got.BundleImages()
	if err != nil || !reflect.DeepEqual(images, []string{"busybox:1.36.1", "docker.io/library/alpine:3.20"}) {
		t.Errorf("bundle images = %v, %v", images, err)
	}
	if v := got.ValuesFiles(); len(v) != 1 || v[0] != filepath.Join(root, "values-airgap.yaml") {
		t.Errorf("values files = %v", v)
	}
	if !reflect.DeepEqual(got.Release.Set, d.Release.Set) || got.Release.Namespace != "web-system" {
		t.Errorf("release = %+v", got.Release)
	}
	if !reflect.DeepEqual(got.Operations, d.Operations) {
		t.Errorf("operations = %+v, want %+v", got.Operations, d.Operations)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
