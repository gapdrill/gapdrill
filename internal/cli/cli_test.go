// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gapdrill/gapdrill/internal/report"
	"github.com/gapdrill/gapdrill/internal/runner"
	"github.com/gapdrill/gapdrill/internal/spec"
)

func execute(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Execute(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestInitWritesADrillThatLoads(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, "charts/web/Chart.yaml", "apiVersion: v2\nname: web\nversion: 1.0.0\n")
	writeFile(t, "images.txt", "busybox:1.36.1\n")

	code, stdout, stderr := execute(t, "init", "--chart", "charts/web", "--images", "images.txt", "--set", "replicas=1")
	if code != runner.ExitPassed {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Wrote gapdrill.yaml") {
		t.Errorf("stdout = %q", stdout)
	}
	d, err := spec.Load(filepath.Join(dir, "gapdrill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Release.Name != "web" || d.Release.Set["replicas"] != "1" || len(d.Operations) != 3 {
		t.Errorf("drill = %+v", d)
	}

	// A second init must not clobber the file the user may have edited.
	code, _, stderr = execute(t, "init", "--chart", "charts/web", "--images", "images.txt")
	if code != runner.ExitUsage || !strings.Contains(stderr, "already exists") {
		t.Errorf("second init: exit %d, %q", code, stderr)
	}
	if code, _, stderr = execute(t, "init", "--chart", "charts/web", "--images", "images.txt", "--force"); code != runner.ExitPassed {
		t.Errorf("init --force: exit %d, %q", code, stderr)
	}
}

func TestRunRejectsInconsistentFlags(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--name", "web"}, "--name needs --chart"},
		{[]string{"run", "--chart", "./web"}, "needs the bundle's images"},
		{[]string{"run", "--chart", "./web", "--image", "busybox:1", "-f", "x.yaml"}, "not both"},
		{[]string{"run", "--chart", "./web", "--image", "busybox:1", "--set", "novalue"}, "want key=value"},
		{[]string{"init", "--images", "images.txt"}, "--chart is required"},
	} {
		code, _, stderr := execute(t, tt.args...)
		if code != runner.ExitUsage || !strings.Contains(stderr, tt.want) {
			t.Errorf("%v: exit %d, stderr %q; want exit %d and %q", tt.args, code, stderr, runner.ExitUsage, tt.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeReport(t *testing.T, path string, r *report.Report) {
	t.Helper()
	r.SchemaVersion = report.SchemaVersion
	b, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(b))
}

func TestCompareExitCodes(t *testing.T) {
	t.Chdir(t.TempDir())
	lookup := report.Finding{ID: "aaaa", Kind: report.NetworkLookup, Host: "pypi.org", Message: "Job hook looked up pypi.org, which doesn't resolve offline."}
	missing := report.Finding{ID: "bbbb", Kind: report.MissingImage, Image: "docker.io/library/busybox:1.37.0", Message: "CronJob cleanup needs docker.io/library/busybox:1.37.0, which isn't in the bundle."}
	run := func(fs ...report.Finding) *report.Report {
		return &report.Report{Drill: "demo", Verdict: report.Failed, Operations: []report.Operation{{Name: "install", Status: report.StatusFailed, Findings: fs}}}
	}
	writeReport(t, "base.json", run(lookup))
	writeReport(t, "same.json", run(lookup))
	writeReport(t, "head.json", run(missing))
	writeReport(t, "errored.json", &report.Report{Drill: "demo", Verdict: report.Errored, Error: "isolation failed"})
	writeFile(t, "other.json", `{"schemaVersion": "something/else"}`)

	if code, out, _ := execute(t, "compare", "base.json", "same.json"); code != runner.ExitPassed || !strings.Contains(out, "No new offline problems") {
		t.Errorf("same findings: exit %d\n%s", code, out)
	}
	code, out, _ := execute(t, "compare", "base.json", "head.json")
	if code != runner.ExitFailed || !strings.Contains(out, "1 new offline problem") || !strings.Contains(out, "1 fixed") {
		t.Errorf("a new finding: exit %d\n%s", code, out)
	}
	if code, out, _ := execute(t, "compare", "errored.json", "head.json"); code != runner.ExitError || !strings.Contains(out, "couldn't run") {
		t.Errorf("errored base: exit %d\n%s", code, out)
	}
	if code, _, stderr := execute(t, "compare", "base.json", "other.json"); code != runner.ExitUsage || !strings.Contains(stderr, "unsupported report schema") {
		t.Errorf("another schema: exit %d\n%s", code, stderr)
	}
}

func TestCompareRequiresCompletedOperationResults(t *testing.T) {
	t.Chdir(t.TempDir())
	// A drill with only skipped operations can still have a passing verdict.
	writeReport(t, "valid.json", &report.Report{
		Drill: "demo", Verdict: report.Passed,
		Operations: []report.Operation{{Name: "chart-tests", Status: report.StatusSkipped}},
	})
	if code, _, stderr := execute(t, "compare", "valid.json", "valid.json"); code != runner.ExitPassed {
		t.Fatalf("skipped operations: exit %d, %q", code, stderr)
	}
	for _, tc := range []struct {
		name, fields, want string
	}{
		{"missing verdict", "", "verdict"},
		{"unknown verdict", `,"verdict":"finished"`, "verdict"},
		{"missing operations", `,"verdict":"passed"`, "operations"},
		{"empty operations", `,"verdict":"failed","operations":[]`, "operations"},
		{"unknown status", `,"verdict":"passed","operations":[{"status":"pending"}]`, "operations[0].status"},
		{"missing status", `,"verdict":"passed","operations":[{}]`, "operations[0].status"},
		{"failure marked passed", `,"verdict":"passed","operations":[{"status":"failed"}]`, "verdict"},
		{"failed without a failed operation", `,"verdict":"failed","operations":[{"status":"passed"}]`, "verdict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, "bad.json", `{"schemaVersion":"`+report.SchemaVersion+`"`+tc.fields+`}`)
			for _, args := range [][]string{{"compare", "bad.json", "valid.json"}, {"compare", "valid.json", "bad.json"}} {
				code, stdout, stderr := execute(t, args...)
				if code != runner.ExitUsage || stdout != "" || !strings.Contains(stderr, "bad.json") || !strings.Contains(stderr, tc.want) {
					t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
				}
			}
		})
	}
}
