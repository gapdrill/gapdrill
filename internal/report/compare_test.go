// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/gapdrill/gapdrill/internal/evidence"
)

// ids lists a report's finding IDs.
func ids(r *Report) map[string]bool {
	out := map[string]bool{}
	for id := range findingsByID(r) {
		out[id] = true
	}
	return out
}

func TestFindingIDsSurviveARerun(t *testing.T) {
	first := Build(demoInput())

	// The same problems in a later run, from pods with other names.
	in := demoInput()
	for i := range in.Evidence {
		if p := in.Evidence[i].Pod; p != nil {
			renamed := *p
			renamed.Name += "-rerun"
			renamed.UID += "-rerun"
			in.Evidence[i].Pod = &renamed
		}
	}
	second := Build(in)

	a, b := ids(first), ids(second)
	if len(a) != 3 {
		t.Fatalf("the demo has 3 distinct findings, got %d", len(a))
	}
	for id := range a {
		if !b[id] {
			t.Errorf("finding %s has another ID in the rerun", id)
		}
	}
}

func TestAMissingImageIsTheSameProblemWhoeverAsked(t *testing.T) {
	cronJob := &evidence.Pod{Namespace: "demo", Name: "c", Owner: "CronJob demo-cleanup"}
	seenByPod := Finding{Kind: MissingImage, Image: "docker.io/library/busybox:1.37.0", Pod: cronJob}
	seenByRegistry := Finding{Kind: MissingImage, Image: "docker.io/library/busybox:1.37.0"}
	if findingID(seenByPod) != findingID(seenByRegistry) {
		t.Error("whether a run saw the pod that asked mustn't change a missing image's identity")
	}
	hook := &evidence.Pod{Owner: "Job demo-post-install (Helm post-install hook)"}
	web := &evidence.Pod{Owner: "Deployment demo-web"}
	if findingID(Finding{Kind: NetworkLookup, Host: "pypi.org", Pod: hook}) == findingID(Finding{Kind: NetworkLookup, Host: "pypi.org", Pod: web}) {
		t.Error("two workloads looking up a host are two problems to fix")
	}
}

func TestCompare(t *testing.T) {
	base := Build(demoInput())

	// The hook is fixed, and something new looks up a telemetry host.
	in := demoInput()
	var kept []evidence.Item
	for _, e := range in.Evidence {
		if e.Pod != nil && strings.HasPrefix(e.Pod.Owner, "Job demo-post-install") {
			continue
		}
		kept = append(kept, e)
	}
	agent := &evidence.Pod{Namespace: "demo", Name: "agent-1", UID: "u-agent", Owner: "Deployment demo-agent"}
	in.Evidence = append(kept, evidence.Item{
		Kind: evidence.DNSQuery, Time: at(20), Pod: agent, Client: netip.MustParseAddr("10.244.0.12"), QName: "telemetry.example.com", QType: "A",
	})
	head := Build(in)

	c := Compare(base, head)
	if len(c.New) != 1 || c.New[0].Host != "telemetry.example.com" {
		t.Errorf("New = %+v", c.New)
	}
	if len(c.Fixed) != 2 { // the hook's lookup of pypi.org and its exit code
		t.Errorf("Fixed = %+v", c.Fixed)
	}
	if c.Unchanged != 1 { // the CronJob's missing image
		t.Errorf("Unchanged = %d", c.Unchanged)
	}

	md := c.Markdown("demo")
	for _, want := range []string{
		"**✗ 1 new offline problem** since the base run. 2 fixed, 1 unchanged.",
		"### New\n\n- Deployment demo-agent looked up telemetry.example.com",
		"  - **Fix:** Nothing answers this name offline.",
		"### Fixed\n\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown lacks %q:\n%s", want, md)
		}
	}

	if c := Compare(base, base); len(c.New) != 0 || len(c.Fixed) != 0 || !strings.Contains(c.Markdown("demo"), "**✓ No new offline problems**") {
		t.Errorf("a run compared with itself: %+v", c)
	}
}
