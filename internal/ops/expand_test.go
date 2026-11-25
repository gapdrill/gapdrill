// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package ops

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/gapdrill/gapdrill/internal/spec"
)

func cronJob(ns, name string) *batchv1.CronJob {
	return &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func TestExpandCronJobsRunsEachOutsideSystemNamespaces(t *testing.T) {
	env := Env{
		Namespace:      "demo",
		SkipNamespaces: []string{"kube-system"},
		Clientset: fake.NewClientset(
			cronJob("demo", "rotate-logs"),
			cronJob("demo", "cleanup"),
			cronJob("operator-made", "compact"), // created at runtime by a controller
			cronJob("kube-system", "not-ours"),
		),
	}
	timeout := spec.Duration(90e9)
	op := spec.Operation{Name: "cronjobs", RunCronJobs: &spec.RunCronJobsOp{Timeout: &timeout}}

	got, skip, err := Expand(context.Background(), env, op)
	if err != nil || skip != "" {
		t.Fatalf("Expand: %v, skip %q", err, skip)
	}
	want := []struct{ name, ns, cj string }{
		{"cronjobs/cleanup", "demo", "cleanup"},
		{"cronjobs/rotate-logs", "demo", "rotate-logs"},
		{"cronjobs/operator-made/compact", "operator-made", "compact"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d operations, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.name || g.RunCronJob == nil || g.RunCronJob.Namespace != w.ns || g.RunCronJob.Name != w.cj {
			t.Errorf("operation %d = %+v, want %+v", i, g, w)
		}
		if g.Timeout() != timeout.Or(0) {
			t.Errorf("operation %d timeout = %s, want the runCronJobs timeout", i, g.Timeout())
		}
	}
}

func TestExpandCronJobsSkipsWhenThereAreNone(t *testing.T) {
	env := Env{Namespace: "demo", SkipNamespaces: []string{"kube-system"}, Clientset: fake.NewClientset(cronJob("kube-system", "x"))}
	got, skip, err := Expand(context.Background(), env, spec.Operation{Name: "cronjobs", RunCronJobs: &spec.RunCronJobsOp{}})
	if err != nil || len(got) != 0 || skip == "" {
		t.Errorf("want a skip reason and no operations, got %+v, %q, %v", got, skip, err)
	}
}

func TestExpandLeavesOtherOperationsAlone(t *testing.T) {
	op := spec.Operation{Name: "install", HelmInstall: &spec.HelmInstallOp{}}
	got, skip, err := Expand(context.Background(), Env{}, op)
	if err != nil || skip != "" || len(got) != 1 || got[0].Name != "install" {
		t.Errorf("got %+v, %q, %v", got, skip, err)
	}
}

func TestHasTestHook(t *testing.T) {
	for _, tt := range []struct {
		name, manifests string
		want            bool
	}{
		{"test hook", "metadata:\n  annotations:\n    helm.sh/hook: test\n", true},
		{"quoted", "metadata:\n  annotations:\n    \"helm.sh/hook\": \"test\"\n", true},
		{"helm 2 name", "    helm.sh/hook: test-success\n", true},
		{"one of several", "    helm.sh/hook: pre-install,test\n", true},
		{"install hook only", "    helm.sh/hook: post-install\n    helm.sh/hook-weight: \"-5\"\n", false},
		{"weight mentions test", "    helm.sh/hook: post-install\n    helm.sh/hook-delete-policy: test\n", false},
		{"no hooks", "", false},
	} {
		if got := hasTestHook(tt.manifests); got != tt.want {
			t.Errorf("%s: hasTestHook = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func waitingOn(reason string) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name:  "app",
		Image: "busybox:1.37.0",
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}},
	}
}

func TestPullWatchWaitsForAStuckPull(t *testing.T) {
	pod := func(reason string) []corev1.Pod {
		return []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "demo", Name: "web"},
			Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{waitingOn(reason)}},
		}}
	}
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	var w pullWatch
	for _, step := range []struct {
		at     time.Duration
		reason string
		want   string
	}{
		{0, "ErrImagePull", ""},
		{10 * time.Second, "ImagePullBackOff", ""},  // failing, but not for long enough
		{15 * time.Second, "ContainerCreating", ""}, // the pull recovered: start over
		{20 * time.Second, "ErrImagePull", ""},
		{39 * time.Second, "ImagePullBackOff", ""}, // 19s since it failed again
		{40 * time.Second, "ImagePullBackOff", "busybox:1.37.0"},
	} {
		if got := w.update(pod(step.reason), t0.Add(step.at)); got != step.want {
			t.Errorf("at %s (%s): update = %q, want %q", step.at, step.reason, got, step.want)
		}
	}
}

func TestPullWatchCoversInitContainers(t *testing.T) {
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Namespace: "demo", Name: "migrate"},
		Status:     corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{waitingOn("ErrImagePull")}},
	}}
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	var w pullWatch
	w.update(pods, t0)
	if got := w.update(pods, t0.Add(pullGrace)); got != "busybox:1.37.0" {
		t.Errorf("update = %q, want the init container's image", got)
	}
}
