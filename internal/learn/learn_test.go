// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package learn

import (
	"reflect"
	"testing"
)

const digest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"

func TestParseImages(t *testing.T) {
	out := `{"images": [
	  {"id": "sha256:a", "repoTags": ["docker.io/library/busybox:1.36.1"], "repoDigests": ["docker.io/library/busybox@` + digest + `"]},
	  {"id": "sha256:b", "repoTags": ["registry.k8s.io/pause:3.10", "registry.k8s.io/pause:latest"], "repoDigests": []},
	  {"id": "sha256:c", "repoTags": [], "repoDigests": ["quay.io/acme/agent@` + digest + `"]},
	  {"id": "sha256:d", "repoTags": ["<none>:<none>"], "repoDigests": []}
	]}`
	got, err := ParseImages(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"docker.io/library/busybox:1.36.1", // listed by tag, not also by digest
		"registry.k8s.io/pause:3.10",
		"registry.k8s.io/pause:latest",
		"quay.io/acme/agent@" + digest, // no tag, so by digest
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseImages =\n%v\nwant\n%v", got, want)
	}
	if _, err := ParseImages("not json"); err == nil {
		t.Error("garbage must be an error, not an empty list")
	}
}

func TestNewLeavesOutWhatTheNodeAlreadyHad(t *testing.T) {
	before := []string{"registry.k8s.io/pause:3.10", "docker.io/kindest/kindnetd:v1"}
	after := []string{
		"docker.io/library/busybox:1.37.0",
		"registry.k8s.io/pause:3.10",
		"docker.io/library/busybox:1.36.1",
		"docker.io/library/busybox:1.37.0", // seen on two nodes
		"docker.io/kindest/kindnetd:v1",
	}
	want := []string{"docker.io/library/busybox:1.36.1", "docker.io/library/busybox:1.37.0"}
	if got := New(before, after); !reflect.DeepEqual(got, want) {
		t.Errorf("New = %v, want %v", got, want)
	}
}
