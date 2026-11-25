// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package mirror

import (
	"strings"
	"testing"
)

func TestParseNormalises(t *testing.T) {
	const digest = "sha256:0000000000000000000000000000000000000000000000000000000000000001"
	tests := []struct {
		in, canonical, mirror string
	}{
		{"busybox", "docker.io/library/busybox:latest", "docker.io/library/busybox"},
		{"busybox:1.36.1", "docker.io/library/busybox:1.36.1", "docker.io/library/busybox"},
		{"index.docker.io/library/busybox:1.36.1", "docker.io/library/busybox:1.36.1", "docker.io/library/busybox"},
		{"docker.io/rancher/rancher-agent:v2.12.2", "docker.io/rancher/rancher-agent:v2.12.2", "docker.io/rancher/rancher-agent"},
		{"quay.io/jetstack/cert-manager-controller:v1.16.0", "quay.io/jetstack/cert-manager-controller:v1.16.0", "quay.io/jetstack/cert-manager-controller"},
		{"localhost:5000/team/app:2", "localhost:5000/team/app:2", "localhost_5000/team/app"},
		{"ghcr.io/acme/app@" + digest, "ghcr.io/acme/app@" + digest, "ghcr.io/acme/app"},
	}
	for _, tt := range tests {
		img, err := Parse(tt.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.in, err)
			continue
		}
		if got := img.String(); got != tt.canonical {
			t.Errorf("Parse(%q).String() = %q, want %q", tt.in, got, tt.canonical)
		}
		if got := img.MirrorRepository(); got != tt.mirror {
			t.Errorf("Parse(%q).MirrorRepository() = %q, want %q", tt.in, got, tt.mirror)
		}
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	for _, in := range []string{"", "Busybox:1", "busybox:", "busybox@sha256:short"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q): want an error", in)
		}
	}
}

func TestMirrorReference(t *testing.T) {
	img, _ := Parse("quay.io/acme/app:1.2")
	if got, want := img.MirrorReference("127.0.0.1:5001"), "127.0.0.1:5001/quay.io/acme/app:1.2"; got != want {
		t.Errorf("MirrorReference = %q, want %q", got, want)
	}
}

func TestLearnHostsFilesFallBackUpstream(t *testing.T) {
	files := LearnHostsFiles("172.18.0.3:5000", []string{"quay.io", "docker.io", "docker.io"})
	if len(files) != 3 {
		t.Fatalf("want _default plus one file per registry, got %d files", len(files))
	}
	for path, content := range files {
		// Without a server line, containerd falls back to the registry being
		// pulled from once the bundle registry has answered "not found".
		if strings.Contains(content, "\nserver") {
			t.Errorf("%s: a learn run must be able to fall back upstream:\n%s", path, content)
		}
		if !strings.Contains(content, `[host."http://172.18.0.3:5000`) {
			t.Errorf("%s: the bundle registry must still be asked first:\n%s", path, content)
		}
	}
	want := "[host.\"http://172.18.0.3:5000/v2/quay.io\"]\n  capabilities = [\"pull\", \"resolve\"]\n  override_path = true\n"
	if !strings.HasSuffix(files["quay.io/hosts.toml"], want) {
		t.Errorf("quay.io/hosts.toml:\n%s", files["quay.io/hosts.toml"])
	}
}

func TestHostsFilesNeverFallBackUpstream(t *testing.T) {
	files := HostsFiles("172.18.0.3:5000", []string{"quay.io", "docker.io", "docker.io", "localhost:5000"})

	var keys []string
	for k := range files {
		keys = append(keys, k)
	}
	if len(files) != 4 {
		t.Fatalf("want _default plus one file per registry, got %v", keys)
	}
	for path, content := range files {
		if !strings.Contains(content, `server = "http://172.18.0.3:5000"`) {
			t.Errorf("%s: server must be the bundle registry so there is no upstream fallback:\n%s", path, content)
		}
	}
	want := `[host."http://172.18.0.3:5000/v2/docker.io"]
  capabilities = ["pull", "resolve"]
  override_path = true
`
	if !strings.HasSuffix(files["docker.io/hosts.toml"], want) {
		t.Errorf("docker.io/hosts.toml:\n%s\nwant suffix:\n%s", files["docker.io/hosts.toml"], want)
	}
	if !strings.Contains(files["localhost:5000/hosts.toml"], `"http://172.18.0.3:5000/v2/localhost_5000"`) {
		t.Errorf("ports must be encoded in the mirror path:\n%s", files["localhost:5000/hosts.toml"])
	}
	if strings.Contains(files["_default/hosts.toml"], "[host.") {
		t.Errorf("_default should only set server:\n%s", files["_default/hosts.toml"])
	}
}
