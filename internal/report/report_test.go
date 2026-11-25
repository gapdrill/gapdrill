// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"errors"
	"flag"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gapdrill/gapdrill/internal/evidence"
)

var update = flag.Bool("update", false, "rewrite golden files")

var t0 = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

func at(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

// demoInput mirrors what the demo drill produces: a post-install hook that
// needs PyPI, a CronJob whose image isn't in the bundle, and passing tests.
func demoInput() Input {
	hook := &evidence.Pod{Namespace: "demo", Name: "demo-post-install-x7k2p", UID: "u-hook", Owner: "Job demo-post-install (Helm post-install hook)"}
	cleanup := &evidence.Pod{Namespace: "demo", Name: "demo-cleanup-gapdrill-1234-q9v", UID: "u-cj", Owner: "CronJob demo-cleanup"}
	ip := netip.MustParseAddr("10.244.0.9")

	ev := []evidence.Item{
		{Kind: evidence.ContainerFailed, Time: at(30), Pod: hook, Container: "post-install", ExitCode: 1, Reason: "Error",
			Logs: []string{"WARNING: Retrying (Retry(total=0...)) after connection broken by 'NameResolutionError(\"Failed to resolve 'pypi.org'\")'", "ERROR: Could not find a version that satisfies the requirement requests (from versions: none)"}},
		{Kind: evidence.ImagePullFailed, Time: at(62), Pod: cleanup, Image: "busybox:1.37.0", Reason: "ErrImagePull", Message: "failed to resolve reference: not found"},
		{Kind: evidence.ImagePullFailed, Time: at(70), Pod: cleanup, Image: "docker.io/library/busybox:1.37.0", Reason: "ImagePullBackOff"},
		{Kind: evidence.ManifestNotFound, Time: at(62), Image: "docker.io/library/busybox:1.37.0"},
	}
	for i := 0; i < 6; i++ {
		qt := "A"
		if i%2 == 1 {
			qt = "AAAA"
		}
		ev = append(ev, evidence.Item{Kind: evidence.DNSQuery, Time: at(20 + i), Pod: hook, Client: ip, QName: "pypi.org", QType: qt, Reason: "NXDOMAIN"})
	}

	return Input{
		Drill: "demo", Version: "v0.1.0",
		Started: t0, Finished: at(120),
		Environment: Environment{NodeImage: "kindest/node:v1.37.0", Platform: "linux/arm64", Helm: "v3.22.1"},
		Isolation:   Isolation{Verified: true, ReachableBefore: true, ProbeURL: "https://1.1.1.1", RejectedPackets: 12},
		Bundle:      []string{"docker.io/library/busybox:1.36.1", "docker.io/library/python:3.12-alpine"},
		Operations: []OperationRun{
			{Name: "install", Type: "helmInstall", Start: at(0), End: at(34), Settle: 5 * time.Second,
				Err:    errors.New("helm failed: INSTALLATION FAILED: failed post-install: 1 error occurred: * job demo-post-install failed: BackoffLimitExceeded"),
				Output: "Release \"demo\" does not exist. Installing it now.\nError: INSTALLATION FAILED: failed post-install: 1 error occurred:\n\t* job demo-post-install failed: BackoffLimitExceeded"},
			{Name: "scheduled-cleanup", Type: "runCronJob", Start: at(40), End: at(100), Settle: 5 * time.Second,
				Err: errors.New("job demo-cleanup-gapdrill-1234 didn't finish within 1m0s")},
			{Name: "chart-tests", Type: "helmTest", Start: at(106), End: at(112), Settle: 5 * time.Second},
		},
		Evidence: ev,
	}
}

func TestBuildDiagnosesEachOperation(t *testing.T) {
	r := Build(demoInput())
	if r.Verdict != Failed || r.FailedCount() != 2 {
		t.Fatalf("verdict = %s with %d failed, want failed with 2", r.Verdict, r.FailedCount())
	}

	install := r.Operations[0]
	if install.Summary != "Job demo-post-install (Helm post-install hook) looked up pypi.org, which doesn't resolve offline." {
		t.Errorf("install summary = %q: a blocked lookup must outrank the container failure it caused", install.Summary)
	}
	if f := install.Findings[0]; f.Kind != NetworkLookup || f.Count != 6 {
		t.Errorf("want one network-lookup finding counting all 6 queries, got %+v", f)
	}

	cleanup := r.Operations[1]
	if cleanup.Summary != "CronJob demo-cleanup needs docker.io/library/busybox:1.37.0, which isn't in the bundle." {
		t.Errorf("cleanup summary = %q", cleanup.Summary)
	}
	if n := len(cleanup.Findings); n != 1 || cleanup.Findings[0].Count != 2 {
		t.Errorf("the two pull failures and the registry 404 are one finding, got %+v", cleanup.Findings)
	}

	if tests := r.Operations[2]; tests.Status != StatusPassed || tests.Summary != "" {
		t.Errorf("chart-tests = %+v", tests)
	}
}

func TestBuildReportsHarnessErrorsSeparately(t *testing.T) {
	in := demoInput()
	in.Operations, in.Evidence = nil, nil
	in.HarnessError = errors.New("isolation failed: https://1.1.1.1 is still reachable from node gapdrill-abc123-control-plane")
	r := Build(in)
	if r.Verdict != Errored || !strings.Contains(r.Markdown(), "The drill couldn't run") {
		t.Fatalf("a broken harness must never look like a failed release: %+v", r)
	}
}

func TestEvidenceOutsideWindowsIsUnattributed(t *testing.T) {
	in := demoInput()
	in.Evidence = append(in.Evidence, evidence.Item{Kind: evidence.DNSQuery, Time: at(300), Client: netip.MustParseAddr("10.244.0.30"), QName: "telemetry.example.com"})
	r := Build(in)
	if len(r.Unattributed) != 1 || r.Unattributed[0].Message != "A pod at 10.244.0.30 looked up telemetry.example.com, which doesn't resolve offline." {
		t.Fatalf("unattributed = %+v", r.Unattributed)
	}
}

// Regression: windows used to overlap by the clock slack, and a CronJob's
// pull failure seen 0.7s into runCronJob was blamed on the install before it.
func TestEvidenceRightAfterAnOperationStartsBelongsToIt(t *testing.T) {
	ms := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Millisecond) }
	cleanup := &evidence.Pod{Namespace: "demo", Name: "c", UID: "u", Owner: "CronJob demo-cleanup"}
	r := Build(Input{
		Bundle: []string{"docker.io/library/busybox:1.36.1"},
		Operations: []OperationRun{
			// The runner starts the next operation as soon as the settle
			// period ends: here, at 10s + 5s.
			{Name: "install", Start: ms(0), End: ms(10_000), Settle: 5 * time.Second},
			{Name: "cleanup", Start: ms(15_000), End: ms(75_000), Settle: 5 * time.Second, Err: errors.New("timed out")},
		},
		Evidence: []evidence.Item{{Kind: evidence.ImagePullFailed, Time: ms(15_700), Pod: cleanup, Image: "busybox:1.37.0"}},
	})
	if n := len(r.Operations[0].Findings); n != 0 {
		t.Errorf("install got %d findings, want none", n)
	}
	if n := len(r.Operations[1].Findings); n != 1 {
		t.Errorf("cleanup got %d findings, want the pull failure", n)
	}
}

func TestWindowsPartitionTheDrill(t *testing.T) {
	runs := []OperationRun{
		{Start: at(0), End: at(10), Settle: 5 * time.Second},
		{Start: at(15), End: at(20), Settle: 5 * time.Second},
	}
	for _, tt := range []struct {
		t    time.Time
		want int
	}{
		{at(0).Add(-500 * time.Millisecond), 0}, // clock slack before the first operation
		{at(14), 0},                             // first operation's settle period
		{at(15), 1},                             // the next operation's start belongs to it
		{at(25).Add(500 * time.Millisecond), 1}, // last operation's settle period plus slack
		{at(27), -1},
	} {
		if got := window(runs, tt.t); got != tt.want {
			t.Errorf("window(%s) = %d, want %d", tt.t.Sub(t0), got, tt.want)
		}
	}
}

func TestSkippedOperationsGetNoEvidence(t *testing.T) {
	cleanup := &evidence.Pod{Namespace: "demo", Name: "c", UID: "u", Owner: "CronJob demo-cleanup"}
	r := Build(Input{
		Bundle: []string{"docker.io/library/busybox:1.36.1"},
		Operations: []OperationRun{
			{Name: "install", Start: at(0), End: at(10), Settle: 5 * time.Second},
			{Name: "chart-tests", Start: at(15), End: at(15), Skipped: "the chart has no tests"},
			{Name: "cronjobs/cleanup", Start: at(15), End: at(40), Settle: 5 * time.Second, Err: errors.New("can't start")},
		},
		Evidence: []evidence.Item{{Kind: evidence.ImagePullFailed, Time: at(15), Pod: cleanup, Image: "busybox:1.37.0"}},
	})

	if op := r.Operations[1]; op.Status != StatusSkipped || op.Summary != "Skipped: the chart has no tests." || len(op.Findings) != 0 {
		t.Errorf("skipped operation = %+v", op)
	}
	if n := len(r.Operations[2].Findings); n != 1 {
		t.Errorf("the pull failure belongs to the CronJob that ran, not the skipped tests; got %d findings", n)
	}
	if r.Verdict != Failed || r.RanCount() != 2 {
		t.Errorf("verdict %s, %d ran", r.Verdict, r.RanCount())
	}

	md := r.Markdown()
	for _, want := range []string{"**✗ Failed:** 1 of 2 operations failed. 1 skipped.", "| chart-tests | – skipped |"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "## chart-tests") {
		t.Error("a skipped operation has nothing to detail")
	}
}

func TestPassingOperationKeepsWarnings(t *testing.T) {
	in := demoInput()
	in.Operations[0].Err = nil
	r := Build(in)
	if got := r.Operations[0].Summary; !strings.HasPrefix(got, "Passed with 2 warnings. First: Job demo-post-install") {
		t.Errorf("summary = %q", got)
	}
}

func TestMarkdownGolden(t *testing.T) {
	got := Build(demoInput()).Markdown()
	path := filepath.Join("testdata", "demo.golden.md")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run `go test ./internal/report -update` to create it)", err)
	}
	if got != string(want) {
		t.Errorf("Markdown differs from %s (run with -update after checking the change):\n%s", path, got)
	}
}

func TestJSONHasSchemaVersion(t *testing.T) {
	b, err := Build(demoInput()).JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"schemaVersion": "gapdrill/report/v1alpha1"`) {
		t.Errorf("missing schema version:\n%s", b)
	}
}
