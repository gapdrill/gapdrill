// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"strings"
	"testing"
	"time"
)

const sampleLog = `time="2026-11-24T10:14:00Z" level=info msg="listening on [::]:5000"
172.18.0.2 - - [24/Nov/2026:10:15:03 +0000] "HEAD /v2/docker.io/library/busybox/manifests/1.36.1?ns=docker.io HTTP/1.1" 200 1612 "" "containerd/v2.3.4"
172.18.0.2 - - [24/Nov/2026:10:15:04 +0000] "HEAD /v2/docker.io/library/busybox/manifests/1.37.0?ns=docker.io HTTP/1.1" 404 96 "" "containerd/v2.3.4"
172.18.0.2 - - [24/Nov/2026:10:15:04 +0000] "HEAD /v2/library/busybox/manifests/1.37.0?ns=docker.io HTTP/1.1" 404 96 "" "containerd/v2.3.4"
172.18.0.2 - - [24/Nov/2026:10:15:09 +0000] "GET /v2/acme/tool/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000001?ns=ghcr.io HTTP/1.1" 404 96 "" "containerd/v2.3.4"
172.17.0.1 - - [24/Nov/2026:10:14:30 +0000] "PUT /v2/docker.io/library/busybox/manifests/1.36.1 HTTP/1.1" 201 0 "" "gapdrill"
172.17.0.1 - - [24/Nov/2026:10:14:29 +0000] "HEAD /v2/docker.io/library/python/manifests/3.12-alpine HTTP/1.1" 404 96 "" "gapdrill go-containerregistry/v0.22.1"
`

func TestParseAccessLog(t *testing.T) {
	reqs := ParseAccessLog(sampleLog)
	if len(reqs) != 5 {
		t.Fatalf("want the 5 GET/HEAD manifest requests, got %d: %+v", len(reqs), reqs)
	}
	if !reqs[1].FromContainerd() || reqs[4].FromContainerd() {
		t.Errorf("crane's existence checks while copying the bundle must not count as node pulls: %+v", reqs[4])
	}
	if r := reqs[1]; r.Status != 404 || r.Repository != "docker.io/library/busybox" || r.Reference != "1.37.0" || r.Namespace != "docker.io" {
		t.Errorf("got %+v", r)
	}
	if want := time.Date(2026, 11, 24, 10, 15, 4, 0, time.UTC); !reqs[1].Time.Equal(want) {
		t.Errorf("time = %s, want %s", reqs[1].Time, want)
	}

	bundle := []string{"docker.io"}
	for i, want := range map[int]string{
		1: "docker.io/library/busybox:1.37.0",
		2: "docker.io/library/busybox:1.37.0",
		3: "ghcr.io/acme/tool@sha256:0000000000000000000000000000000000000000000000000000000000000001",
	} {
		if got := reqs[i].UpstreamReference(bundle); got != want {
			t.Errorf("request %d: UpstreamReference = %q, want %q", i, got, want)
		}
	}
}

func TestWithoutDigestFollowUps(t *testing.T) {
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	const (
		index    = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		platform = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
		pinned   = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	)
	reqs := []Request{
		{Time: t0, Repository: "docker.io/library/busybox", Reference: "1.37.0"},
		{Time: t0.Add(time.Second), Repository: "docker.io/library/busybox", Reference: index},        // follow-up
		{Time: t0.Add(2 * time.Second), Repository: "docker.io/library/busybox", Reference: platform}, // follow-up
		{Time: t0, Repository: "quay.io/acme/agent", Reference: pinned},                               // pinned by digest: no tag before it
		{Time: t0.Add(5 * time.Minute), Repository: "docker.io/library/busybox", Reference: pinned},   // long after: a different pull
	}
	var got []string
	for _, r := range WithoutDigestFollowUps(reqs) {
		got = append(got, r.Repository+" "+r.Reference)
	}
	want := []string{
		"docker.io/library/busybox 1.37.0",
		"quay.io/acme/agent " + pinned,
		"docker.io/library/busybox " + pinned,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("WithoutDigestFollowUps =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
