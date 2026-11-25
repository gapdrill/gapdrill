// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package dnsmon

import (
	"strings"
	"testing"
	"time"
)

func TestCorefileSeparatesClusterZone(t *testing.T) {
	c := Corefile()
	cluster := strings.Index(c, "cluster.local:53")
	catchAll := strings.Index(c, ".:53 {")
	if cluster < 0 || catchAll < 0 || cluster > catchAll {
		t.Fatalf("want a cluster.local server block before the catch-all block:\n%s", c)
	}
	clusterBlock := c[cluster:catchAll]
	if strings.Contains(clusterBlock, "template") {
		t.Error("template must not share a block with kubernetes: it runs first and would answer cluster names")
	}
	if strings.Contains(c, "forward") {
		t.Error("an offline cluster must not forward queries upstream")
	}
	for _, want := range []string{"health {", "ready", "rcode NXDOMAIN", `log . "gapdrill-dns {remote} {type} {name} {rcode}"`} {
		if !strings.Contains(c[catchAll:], want) {
			t.Errorf("catch-all block is missing %q", want)
		}
	}
}

func TestParseLine(t *testing.T) {
	q, ok := ParseLine("2026-11-24T10:15:03.123456789Z [INFO] gapdrill-dns 10.244.0.7 AAAA pypi.org. NXDOMAIN")
	if !ok {
		t.Fatal("want a query")
	}
	if q.Client.String() != "10.244.0.7" || q.Type != "AAAA" || q.Name != "pypi.org" || q.RCode != "NXDOMAIN" {
		t.Errorf("got %+v", q)
	}
	if want := time.Date(2026, 11, 24, 10, 15, 3, 123456789, time.UTC); !q.Time.Equal(want) {
		t.Errorf("time = %s, want %s", q.Time, want)
	}

	for _, line := range []string{
		"2026-11-24T10:15:03Z .:53",
		"2026-11-24T10:15:03Z [INFO] plugin/reload: Running configuration SHA512 = abc",
		"not-a-time [INFO] gapdrill-dns 10.244.0.7 A pypi.org. NXDOMAIN",
		"",
	} {
		if _, ok := ParseLine(line); ok {
			t.Errorf("ParseLine(%q): want no query", line)
		}
	}
}

func TestDropSearchExpansions(t *testing.T) {
	names := []string{"pypi.org.corp.example", "pypi.org", "api.corp.example", "files.pythonhosted.org"}
	got := DropSearchExpansions(names, []string{"corp.example."})
	want := "pypi.org,api.corp.example,files.pythonhosted.org"
	if strings.Join(got, ",") != want {
		t.Errorf("got %v, want %s (api.corp.example stays: its base name was never queried)", got, want)
	}
}
