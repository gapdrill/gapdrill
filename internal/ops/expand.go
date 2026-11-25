// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package ops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gapdrill/gapdrill/internal/spec"
)

// Expand returns the operations to run for op. Operations that depend on
// what's in the cluster when they start (runCronJobs, and helmTest with
// ifPresent) are worked out now; others come back unchanged. A non-empty
// skip reason means there's nothing to run.
func Expand(ctx context.Context, env Env, op spec.Operation) ([]spec.Operation, string, error) {
	switch {
	case op.RunCronJobs != nil:
		return expandCronJobs(ctx, env, op)
	case op.HelmTest != nil && op.HelmTest.IfPresent:
		has, err := HasTests(ctx, env)
		if err != nil {
			return nil, "", err
		}
		if !has {
			return nil, "the chart has no tests", nil
		}
	}
	return []spec.Operation{op}, "", nil
}

// expandCronJobs turns a runCronJobs operation into one runCronJob per
// CronJob, named "<operation>/<CronJob>", or "<operation>/<namespace>/<CronJob>"
// outside the release namespace.
func expandCronJobs(ctx context.Context, env Env, op spec.Operation) ([]spec.Operation, string, error) {
	list, err := env.Clientset.BatchV1().CronJobs(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("list CronJobs: %w", err)
	}
	skip := make(map[string]bool, len(env.SkipNamespaces))
	for _, ns := range env.SkipNamespaces {
		skip[ns] = true
	}
	var cjs []batchv1.CronJob
	for _, cj := range list.Items {
		if !skip[cj.Namespace] {
			cjs = append(cjs, cj)
		}
	}
	if len(cjs) == 0 {
		return nil, "the cluster has no CronJobs", nil
	}
	sort.Slice(cjs, func(i, j int) bool {
		if cjs[i].Namespace != cjs[j].Namespace {
			return cjs[i].Namespace < cjs[j].Namespace
		}
		return cjs[i].Name < cjs[j].Name
	})

	out := make([]spec.Operation, len(cjs))
	for i, cj := range cjs {
		name := op.Name + "/" + cj.Name
		if cj.Namespace != env.Namespace {
			name = op.Name + "/" + cj.Namespace + "/" + cj.Name
		}
		out[i] = spec.Operation{
			Name:   name,
			Settle: op.Settle,
			RunCronJob: &spec.RunCronJobOp{
				Name:      cj.Name,
				Namespace: cj.Namespace,
				Timeout:   op.RunCronJobs.Timeout,
			},
		}
	}
	return out, "", nil
}

// HasTests reports whether the release's chart has test hooks.
func HasTests(ctx context.Context, env Env) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, env.HelmBin, "get", "hooks", env.Release,
		"--namespace", env.Namespace, "--kubeconfig", env.Kubeconfig).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			err = errors.New(strings.TrimSpace(string(ee.Stderr)))
		}
		return false, fmt.Errorf("list the release's hooks: %w", err)
	}
	return hasTestHook(string(out)), nil
}

// hookAnnotation matches a helm.sh/hook annotation and captures its value. It
// doesn't match helm.sh/hook-weight or helm.sh/hook-delete-policy.
var hookAnnotation = regexp.MustCompile(`(?m)^[ \t]*["']?helm\.sh/hook["']?[ \t]*:[ \t]*(.*)$`)

// hasTestHook reports whether rendered hook manifests include a test hook
// ("test", or Helm 2's "test-success" and "test-failure").
func hasTestHook(manifests string) bool {
	for _, m := range hookAnnotation.FindAllStringSubmatch(manifests, -1) {
		for _, hook := range strings.Split(strings.Trim(strings.TrimSpace(m[1]), `"'`), ",") {
			switch strings.TrimSpace(hook) {
			case "test", "test-success", "test-failure":
				return true
			}
		}
	}
	return false
}
