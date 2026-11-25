// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gapdrill/gapdrill/internal/spec"
)

func TestLearnedImages(t *testing.T) {
	const platformDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	bundle := []string{"docker.io/library/busybox:1.36.1", "docker.io/library/python:3.12-alpine"}
	digests := map[string]string{"docker.io/library/python:3.12-alpine": platformDigest}
	pulled := []string{
		"docker.io/library/busybox:1.36.1",
		"docker.io/library/busybox:1.37.0",
		"docker.io/library/python@" + platformDigest, // by its digest in the bundle registry
	}
	want := []LearnedImage{
		{Image: "docker.io/library/busybox:1.36.1", InBundle: true},
		{Image: "docker.io/library/busybox:1.37.0", InBundle: false},
		{Image: "docker.io/library/python@" + platformDigest, InBundle: true},
	}
	if got := learnedImages(bundle, digests, pulled); !reflect.DeepEqual(got, want) {
		t.Errorf("learnedImages =\n%+v\nwant\n%+v", got, want)
	}
}

func TestLearnRunReport(t *testing.T) {
	in := demoInput()
	in.Isolation = Isolation{Learn: true, ProbeURL: "https://1.1.1.1"}
	in.LearnedPulled = []string{"docker.io/library/busybox:1.36.1", "docker.io/library/busybox:1.37.0", "docker.io/library/python:3.12-alpine"}
	r := Build(in)
	if len(r.Learned) != 3 || r.Learned[1].InBundle || !r.Learned[0].InBundle {
		t.Fatalf("Learned = %+v", r.Learned)
	}

	md := r.Markdown()
	for _, want := range []string{
		"## Images the release pulled",
		"- `docker.io/library/busybox:1.37.0` (not in the bundle)",
		"- Learn run: pods could reach only the cluster",
		"The probe check doesn't apply to a learn run",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Isolation was not verified") || strings.Contains(md, "with no upstream fallback") {
		t.Errorf("a learn run must not be described as a strict one:\n%s", md)
	}

	// The file must work as a drill's bundle list as it is.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "images.learned.txt"), r.LearnedFile(), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &spec.Drill{Dir: dir, Bundle: spec.Bundle{ImagesFile: "images.learned.txt"}}
	images, err := d.BundleImages()
	if err != nil || !reflect.DeepEqual(images, in.LearnedPulled) {
		t.Errorf("images.learned.txt loads as %v (%v), want %v:\n%s", images, err, in.LearnedPulled, r.LearnedFile())
	}

	in.HarnessError = errors.New("isolation failed")
	if r := Build(in); len(r.Learned) != 0 {
		t.Errorf("a drill that couldn't run learned nothing: %+v", r.Learned)
	}
}
