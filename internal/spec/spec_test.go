// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validDrill = `
apiVersion: gapdrill/v1alpha1
kind: Drill
metadata:
  name: demo
bundle:
  imagesFile: images.txt
  images: [docker.io/library/busybox:1.37.0]
release:
  chart: ./chart
  name: demo
  valuesFiles: [values-offline.yaml]
operations:
  - name: install
    helmInstall:
      timeout: 2m
  - name: cleanup
    runCronJob:
      name: demo-cleanup
    settle: 10s
  - name: tests
    helmTest: {}
  - name: custom
    exec:
      command: [kubectl, get, pods]
`

func TestParseValid(t *testing.T) {
	d, err := Parse([]byte(validDrill))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := d.Release.Namespace; got != "demo" {
		t.Errorf("namespace defaults to the release name: got %q", got)
	}
	if got := d.Operations[1].RunCronJob.Namespace; got != "demo" {
		t.Errorf("runCronJob namespace defaults to the release namespace: got %q", got)
	}
	tests := []struct {
		op              int
		typ             string
		timeout, settle time.Duration
	}{
		{0, TypeHelmInstall, 2 * time.Minute, DefaultSettle},
		{1, TypeRunCronJob, DefaultOperationTimeout, 10 * time.Second},
		{2, TypeHelmTest, DefaultOperationTimeout, DefaultSettle},
		{3, TypeExec, DefaultOperationTimeout, DefaultSettle},
	}
	for _, tt := range tests {
		op := d.Operations[tt.op]
		if op.Type() != tt.typ || op.Timeout() != tt.timeout || op.SettlePeriod() != tt.settle {
			t.Errorf("operation %q: got type=%s timeout=%s settle=%s, want %s %s %s",
				op.Name, op.Type(), op.Timeout(), op.SettlePeriod(), tt.typ, tt.timeout, tt.settle)
		}
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte(strings.Replace(validDrill, "imagesFile:", "imageFile:", 1)))
	if err == nil || !strings.Contains(err.Error(), "imageFile") {
		t.Fatalf("want an error naming the unknown field, got %v", err)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	bad := `
apiVersion: gapdrill/v1
kind: Drill
metadata:
  name: Demo
release:
  name: demo
operations:
  - name: tests
    helmTest: {}
  - name: tests
    exec: {command: []}
  - name: both
    helmInstall: {}
    helmTest: {}
  - name: none
`
	_, err := Parse([]byte(bad))
	if err == nil {
		t.Fatal("want validation errors")
	}
	for _, want := range []string{
		"apiVersion:",
		"metadata.name:",
		"bundle:",
		"release.chart: is required",
		"operations[0].helmTest: needs a helmInstall",
		`operations[1].name: "tests" is used by an earlier operation`,
		"operations[1].exec.command: is required",
		"operations[2]: set only one",
		"operations[3]: set one of",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestDurationErrors(t *testing.T) {
	for _, in := range []string{"timeout: 5", `timeout: "soon"`, `timeout: "-1s"`} {
		doc := strings.Replace(validDrill, "timeout: 2m", in, 1)
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: want an error", in)
		}
	}
}

func TestLoadResolvesPathsAndBundle(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("gapdrill.yaml", validDrill)
	write("chart/Chart.yaml", "name: demo\n")
	write("images.txt", "# offline bundle\ndocker.io/library/python:3.12-alpine\n\nbusybox:1.36.1  # trailing comment\ndocker.io/library/busybox:1.37.0\n")

	d, err := Load(filepath.Join(dir, "gapdrill.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := d.ChartRef(), filepath.Join(dir, "chart"); got != want {
		t.Errorf("ChartRef = %q, want %q", got, want)
	}
	if got, want := d.ValuesFiles()[0], filepath.Join(dir, "values-offline.yaml"); got != want {
		t.Errorf("ValuesFiles[0] = %q, want %q", got, want)
	}
	imgs, err := d.BundleImages()
	if err != nil {
		t.Fatalf("BundleImages: %v", err)
	}
	want := []string{"docker.io/library/python:3.12-alpine", "busybox:1.36.1", "docker.io/library/busybox:1.37.0"}
	if strings.Join(imgs, ",") != strings.Join(want, ",") {
		t.Errorf("BundleImages = %v, want %v (file first, inline after, no duplicates)", imgs, want)
	}
}

func TestChartRefPassesRemoteReferencesThrough(t *testing.T) {
	d := &Drill{Dir: t.TempDir(), Release: Release{Chart: "oci://ghcr.io/acme/charts/demo"}}
	if got := d.ChartRef(); got != "oci://ghcr.io/acme/charts/demo" {
		t.Errorf("ChartRef = %q", got)
	}
	d.Release.Chart = "acme/demo"
	if got := d.ChartRef(); got != "acme/demo" {
		t.Errorf("ChartRef = %q", got)
	}
}

func TestImageListRejectsTwoRefsOnALine(t *testing.T) {
	if _, err := parseImageList([]byte("busybox:1 alpine:3\n")); err == nil {
		t.Fatal("want an error")
	}
}
