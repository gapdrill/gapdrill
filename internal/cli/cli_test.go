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
