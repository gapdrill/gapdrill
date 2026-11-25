// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

// Package e2e runs the demo drills against real kind clusters. It needs
// Docker and Helm:
//
//	go test -tags e2e -count=1 -timeout 30m ./test/e2e/...
package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gapdrill/gapdrill/internal/report"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gapdrill-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bin = filepath.Join(dir, "gapdrill")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/gapdrill")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build gapdrill:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

var demoDir = filepath.Join("..", "..", "examples", "demo")

// drill runs a demo drill file and returns its report and exit code.
func drill(t *testing.T, file string) (*report.Report, int) {
	t.Helper()
	return gapdrill(t, "-f", filepath.Join(demoDir, file))
}

// gapdrill runs `gapdrill run` with args and returns its report and exit
// code.
func gapdrill(t *testing.T, args ...string) (*report.Report, int) {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(bin, append([]string{"run", "--verbose", "--report-dir", dir}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run gapdrill: %v", err)
		}
		code = ee.ExitCode()
	}
	b, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatalf("no report.json (exit code %d): %v", code, err)
	}
	var r report.Report
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("parse report.json: %v", err)
	}
	return &r, code
}

func operation(t *testing.T, r *report.Report, name string) report.Operation {
	t.Helper()
	for _, op := range r.Operations {
		if op.Name == name {
			return op
		}
	}
	t.Fatalf("no operation %q in the report", name)
	return report.Operation{}
}

func hasFinding(op report.Operation, kind report.FindingKind, match func(report.Finding) bool) bool {
	for _, f := range op.Findings {
		if f.Kind == kind && match(f) {
			return true
		}
	}
	return false
}

func TestDemoDrillCatchesBothOfflineBugs(t *testing.T) {
	r, code := drill(t, "gapdrill.yaml")
	if code != 1 {
		t.Fatalf("exit code %d, want 1 (operations failed); verdict %s %s", code, r.Verdict, r.Error)
	}
	if !r.Isolation.Verified {
		t.Error("isolation wasn't verified")
	}

	install := operation(t, r, "install")
	if install.Status != report.StatusFailed || !hasFinding(install, report.NetworkLookup, func(f report.Finding) bool { return f.Host == "pypi.org" }) {
		t.Errorf("install should fail on the hook's lookup of pypi.org: %+v", install)
	}

	cleanup := operation(t, r, "scheduled-cleanup")
	if cleanup.Status != report.StatusFailed || !hasFinding(cleanup, report.MissingImage, func(f report.Finding) bool {
		return f.Image == "docker.io/library/busybox:1.37.0" && f.Pod != nil && f.Pod.Owner == "CronJob demo-cleanup"
	}) {
		t.Errorf("scheduled-cleanup should fail on the missing cleanup image: %+v", cleanup)
	}

	if tests := operation(t, r, "chart-tests"); tests.Status != report.StatusPassed {
		t.Errorf("chart-tests should pass: %+v", tests)
	}
}

// With no drill file, gapdrill must find the CronJob itself.
func TestZeroConfigDrillFindsTheCronJobItself(t *testing.T) {
	r, code := gapdrill(t, "--chart", filepath.Join(demoDir, "chart"), "--images", filepath.Join(demoDir, "images.txt"), "--name", "demo")
	if code != 1 {
		t.Fatalf("exit code %d, want 1 (operations failed); verdict %s %s", code, r.Verdict, r.Error)
	}

	install := operation(t, r, "install")
	if install.Status != report.StatusFailed || !hasFinding(install, report.NetworkLookup, func(f report.Finding) bool { return f.Host == "pypi.org" }) {
		t.Errorf("install should fail on the hook's lookup of pypi.org: %+v", install)
	}

	cleanup := operation(t, r, "cronjobs/demo-cleanup")
	if cleanup.Status != report.StatusFailed || cleanup.Type != "runCronJob" || !hasFinding(cleanup, report.MissingImage, func(f report.Finding) bool {
		return f.Image == "docker.io/library/busybox:1.37.0"
	}) {
		t.Errorf("the discovered CronJob should fail on its missing image: %+v", cleanup)
	}
	if cleanup.DurationSeconds > 60 {
		t.Errorf("a Job that can't pull its image should fail fast, not after %.0fs", cleanup.DurationSeconds)
	}

	if tests := operation(t, r, "chart-tests"); tests.Status != report.StatusPassed {
		t.Errorf("chart-tests should pass: %+v", tests)
	}
}

func TestFixedDemoDrillPasses(t *testing.T) {
	r, code := drill(t, "gapdrill-fixed.yaml")
	if code != 0 || r.Verdict != report.Passed {
		t.Fatalf("exit code %d, verdict %s; want 0 and passed: %+v", code, r.Verdict, r.Operations)
	}
}
