// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bufio"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gapdrill/gapdrill/internal/mirror"
)

// Request is a manifest request from the registry's access log.
type Request struct {
	Time       time.Time
	Repository string
	Reference  string
	// Namespace is the upstream registry containerd asked for (the "ns" query
	// parameter it adds when pulling through a mirror).
	Namespace string
	Status    int
	UserAgent string
}

// FromContainerd reports whether a kind node made the request, as opposed to
// gapdrill copying the bundle in.
func (r Request) FromContainerd() bool { return strings.HasPrefix(r.UserAgent, "containerd") }

// Combined Log Format, as written by Distribution's access log.
var accessRE = regexp.MustCompile(`\[([^\]]+)\] "(?:GET|HEAD) /v2/(.+)/manifests/([^ ?"]+)(?:\?([^ "]*))? HTTP/[0-9.]+" (\d{3}) \S+ "[^"]*" "([^"]*)"`)

// followUpWindow bounds how long after a missed tag request containerd's
// requests for the same image by digest are treated as part of that pull.
const followUpWindow = 30 * time.Second

// WithoutDigestFollowUps drops digest requests that follow a request by tag
// for the same repository within followUpWindow. When a pull by tag misses
// the bundle registry, containerd's next requests by digest miss too, and in
// a learn run they're for the image it then fetches upstream: one missing
// image, not three. A digest request with no tag request before it, such as a
// pull of an image pinned by digest, is kept. reqs must be in log order.
func WithoutDigestFollowUps(reqs []Request) []Request {
	lastTag := map[string]time.Time{}
	var out []Request
	for _, r := range reqs {
		repo := r.Namespace + "/" + r.Repository
		if !strings.Contains(r.Reference, ":") { // tags can't contain ':'; digests do
			lastTag[repo] = r.Time
			out = append(out, r)
			continue
		}
		if t, ok := lastTag[repo]; ok && !r.Time.Before(t) && r.Time.Sub(t) <= followUpWindow {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ParseAccessLog returns the manifest requests in a registry log, skipping
// every other line.
func ParseAccessLog(log string) []Request {
	var reqs []Request
	s := bufio.NewScanner(strings.NewReader(log))
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		m := accessRE.FindStringSubmatch(s.Text())
		if m == nil {
			continue
		}
		t, err := time.Parse("02/Jan/2006:15:04:05 -0700", m[1])
		if err != nil {
			continue
		}
		q, _ := url.ParseQuery(m[4])
		reqs = append(reqs, Request{
			Time:       t,
			Repository: m[2],
			Reference:  m[3],
			Namespace:  q.Get("ns"),
			Status:     atoi(m[5]),
			UserAgent:  m[6],
		})
	}
	return reqs
}

// UpstreamReference rebuilds the image reference a request was for. bundle
// lists the bundle's registries, so that mirror paths such as
// "quay.io/acme/app" map back to "quay.io/acme/app". Requests that fell
// through to the registry root use the "ns" parameter instead.
func (r Request) UpstreamReference(bundleRegistries []string) string {
	repo := r.Repository
	registry := r.Namespace
	for _, reg := range bundleRegistries {
		if rest, ok := strings.CutPrefix(repo, mirror.MirrorPrefix(reg)+"/"); ok {
			registry, repo = reg, rest
			break
		}
	}
	if registry == "" {
		registry = "docker.io"
	}
	sep := ":"
	if strings.Contains(r.Reference, ":") {
		sep = "@"
	}
	return registry + "/" + repo + sep + r.Reference
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
