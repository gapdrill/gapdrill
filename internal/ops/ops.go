// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package ops runs drill operations against the offline cluster.
package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/gapdrill/gapdrill/internal/spec"
)

// Env is what operations need to reach the release.
type Env struct {
	Kubeconfig  string
	HelmBin     string
	ChartRef    string
	Release     string
	Namespace   string
	ValuesFiles []string
	Set         map[string]string
	// Dir is the drill file's directory; exec operations run there.
	Dir       string
	Clientset kubernetes.Interface
	// SkipNamespaces hold the cluster's own components; runCronJobs leaves
	// their CronJobs alone.
	SkipNamespaces []string
}

// Result is an operation's outcome. Output is the tail of what it printed.
type Result struct {
	Output string
	Err    error
}

// outputLines is how much tool output a report keeps per operation.
const outputLines = 40

// Run runs op.
func Run(ctx context.Context, env Env, op spec.Operation) Result {
	timeout := op.Timeout()
	switch {
	case op.HelmInstall != nil:
		return helmInstall(ctx, env, timeout)
	case op.HelmTest != nil:
		return helmTest(ctx, env, timeout)
	case op.RunCronJob != nil:
		return runCronJob(ctx, env, op.RunCronJob, timeout)
	case op.Exec != nil:
		return runTool(ctx, timeout, env.Dir, []string{
			"KUBECONFIG=" + env.Kubeconfig,
			"GAPDRILL_RELEASE=" + env.Release,
			"GAPDRILL_NAMESPACE=" + env.Namespace,
		}, op.Exec.Command[0], op.Exec.Command[1:]...)
	}
	return Result{Err: fmt.Errorf("operation %q has no type", op.Name)}
}

// HelmInstallArgs returns the arguments for installing the release. Helm
// runs on the host, which is online; only the cluster is offline.
func HelmInstallArgs(env Env, timeout time.Duration) []string {
	args := []string{
		"upgrade", "--install", env.Release, env.ChartRef,
		"--namespace", env.Namespace, "--create-namespace",
		"--wait", "--timeout", timeout.String(),
		"--kubeconfig", env.Kubeconfig,
	}
	for _, f := range env.ValuesFiles {
		args = append(args, "--values", f)
	}
	keys := make([]string, 0, len(env.Set))
	for k := range env.Set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--set", k+"="+env.Set[k])
	}
	return args
}

func helmInstall(ctx context.Context, env Env, timeout time.Duration) Result {
	// Helm enforces its own --timeout; the extra minute only guards against
	// Helm itself hanging.
	return runTool(ctx, timeout+time.Minute, env.Dir, nil, env.HelmBin, HelmInstallArgs(env, timeout)...)
}

func helmTest(ctx context.Context, env Env, timeout time.Duration) Result {
	return runTool(ctx, timeout+time.Minute, env.Dir, nil, env.HelmBin,
		"test", env.Release, "--namespace", env.Namespace, "--timeout", timeout.String(), "--kubeconfig", env.Kubeconfig, "--logs")
}

func runTool(ctx context.Context, limit time.Duration, dir string, extraEnv []string, name string, args ...string) Result {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.WaitDelay = 10 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	output := Tail(out.String(), outputLines)
	tool := filepath.Base(name)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return Result{Output: output, Err: fmt.Errorf("%s timed out after %s", tool, limit)}
	case err != nil:
		return Result{Output: output, Err: fmt.Errorf("%s failed: %s", tool, errorLine(output, err))}
	}
	return Result{Output: output}
}

// errorLine picks the most telling part of a failed tool's output: Helm's
// last "Error: …" line and the detail lines after it, else the last line.
func errorLine(output string, err error) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, "Error:") {
			parts := []string{strings.TrimSpace(strings.TrimPrefix(l, "Error:"))}
			for _, more := range lines[i+1:] {
				if more = strings.TrimSpace(more); more != "" {
					parts = append(parts, more)
				}
			}
			return strings.Join(parts, " ")
		}
	}
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return last
	}
	return err.Error()
}

// Tail returns the last n lines of s.
func Tail(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func runCronJob(ctx context.Context, env Env, op *spec.RunCronJobOp, timeout time.Duration) Result {
	jobs := env.Clientset.BatchV1().Jobs(op.Namespace)
	cj, err := env.Clientset.BatchV1().CronJobs(op.Namespace).Get(ctx, op.Name, metav1.GetOptions{})
	if err != nil {
		return Result{Err: fmt.Errorf("get CronJob %s/%s: %w", op.Namespace, op.Name, err)}
	}
	created, err := jobs.Create(ctx, JobFromCronJob(cj, time.Now()), metav1.CreateOptions{})
	if err != nil {
		return Result{Err: fmt.Errorf("create a Job from CronJob %s: %w", op.Name, err)}
	}
	out := fmt.Sprintf("job.batch/%s created from cronjob/%s\n", created.Name, cj.Name)

	var last *batchv1.Job
	var stuckSince time.Time
	var stuckImage string
	err = wait.PollUntilContextTimeout(ctx, time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		j, err := jobs.Get(ctx, created.Name, metav1.GetOptions{})
		if err != nil {
			return false, nil // transient; keep polling
		}
		last = j
		if condition(j, batchv1.JobComplete) != nil || condition(j, batchv1.JobFailed) != nil {
			return true, nil
		}
		if image := jobPullFailure(ctx, env, j); image != "" {
			if stuckSince.IsZero() {
				stuckSince = time.Now()
			}
			stuckImage = image
			return time.Since(stuckSince) >= pullGrace, nil
		}
		stuckSince = time.Time{}
		return false, nil
	})
	if err != nil {
		return Result{Output: out, Err: fmt.Errorf("job %s didn't finish within %s", created.Name, timeout)}
	}
	if c := condition(last, batchv1.JobFailed); c != nil {
		return Result{Output: out, Err: fmt.Errorf("job %s failed: %s", created.Name, strings.TrimSpace(c.Reason+": "+c.Message))}
	}
	if condition(last, batchv1.JobComplete) == nil {
		return Result{Output: out, Err: fmt.Errorf("job %s can't start: %s can't be pulled", created.Name, stuckImage)}
	}
	return Result{Output: out + fmt.Sprintf("job.batch/%s completed\n", created.Name)}
}

// pullGrace is how long a Job's pod may fail to pull an image before the
// operation fails. Offline, a pull that fails keeps failing, so waiting out
// the whole timeout only slows the drill down.
const pullGrace = 20 * time.Second

// jobPullFailure returns an image one of the Job's pods can't pull, or "".
func jobPullFailure(ctx context.Context, env Env, j *batchv1.Job) string {
	pods, err := env.Clientset.CoreV1().Pods(j.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: batchv1.JobNameLabel + "=" + j.Name,
	})
	if err != nil {
		return ""
	}
	for i := range pods.Items {
		if image := pullFailure(&pods.Items[i]); image != "" {
			return image
		}
	}
	return ""
}

// pullFailure returns the image a pod's container is waiting for because it
// can't be pulled, or "".
func pullFailure(p *corev1.Pod) string {
	for _, statuses := range [][]corev1.ContainerStatus{p.Status.InitContainerStatuses, p.Status.ContainerStatuses} {
		for _, s := range statuses {
			if w := s.State.Waiting; w != nil {
				switch w.Reason {
				case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
					return s.Image
				}
			}
		}
	}
	return ""
}

// JobFromCronJob builds the Job that `kubectl create job --from=cronjob/NAME`
// would.
func JobFromCronJob(cj *batchv1.CronJob, now time.Time) *batchv1.Job {
	annotations := map[string]string{"cronjob.kubernetes.io/instantiate": "manual"}
	for k, v := range cj.Spec.JobTemplate.Annotations {
		annotations[k] = v
	}
	suffix := fmt.Sprintf("-gapdrill-%d", now.Unix()%100000)
	base := cj.Name
	if limit := 63 - len(suffix); len(base) > limit {
		base = strings.TrimRight(base[:limit], "-.")
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            base + suffix,
			Namespace:       cj.Namespace,
			Labels:          cj.Spec.JobTemplate.Labels,
			Annotations:     annotations,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cj, batchv1.SchemeGroupVersion.WithKind("CronJob"))},
		},
		Spec: *cj.Spec.JobTemplate.Spec.DeepCopy(),
	}
}

func condition(j *batchv1.Job, t batchv1.JobConditionType) *batchv1.JobCondition {
	if j == nil {
		return nil
	}
	for i := range j.Status.Conditions {
		if c := &j.Status.Conditions[i]; c.Type == t && c.Status == "True" {
			return c
		}
	}
	return nil
}
