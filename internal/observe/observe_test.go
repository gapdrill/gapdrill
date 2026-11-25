// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package observe

import (
	"context"
	"net/netip"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/gapdrill/gapdrill/internal/evidence"
)

func controller(kind, name string) []metav1.OwnerReference {
	yes := true
	return []metav1.OwnerReference{{Kind: kind, Name: name, Controller: &yes, APIVersion: "v1"}}
}

func TestOwnersAreDescribedTheWayUsersKnowThem(t *testing.T) {
	cs := fake.NewClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "demo-cleanup-29000", Namespace: "demo", OwnerReferences: controller("CronJob", "demo-cleanup")}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "demo-post-install", Namespace: "demo", Annotations: map[string]string{"helm.sh/hook": "post-install"}}},
	)
	o := New(cs, t.Logf) // not started: owners come from the API fallback
	ctx := context.Background()

	tests := []struct {
		pod  *corev1.Pod
		want string
	}{
		{&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "demo", OwnerReferences: controller("Job", "demo-cleanup-29000")}}, "CronJob demo-cleanup"},
		{&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p2", Namespace: "demo", OwnerReferences: controller("Job", "demo-post-install")}}, "Job demo-post-install (Helm post-install hook)"},
		{&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "demo-test", Namespace: "demo", Annotations: map[string]string{"helm.sh/hook": "test"}}}, "Pod demo-test (Helm test hook)"},
		{&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p4", Namespace: "demo", OwnerReferences: controller("StatefulSet", "db")}}, "StatefulSet db"},
	}
	for _, tt := range tests {
		if got := o.describeOwner(ctx, tt.pod); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.pod.Name, got, tt.want)
		}
	}
}

func TestPodForIPPicksThePodHoldingTheAddressAtThatTime(t *testing.T) {
	o := New(fake.NewClientset(), t.Logf)
	ip := netip.MustParseAddr("10.244.0.7")
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

	first := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "demo", UID: "u1", CreationTimestamp: metav1.NewTime(t0)},
		Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: ip.String()}}}}
	second := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "demo", UID: "u2", CreationTimestamp: metav1.NewTime(t0.Add(time.Minute))},
		Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: ip.String()}}}}

	o.trackIPs(first, evidence.Pod{Name: "old", UID: "u1"}, t0, false)
	o.trackIPs(first, evidence.Pod{Name: "old", UID: "u1"}, t0.Add(30*time.Second), true)
	o.trackIPs(second, evidence.Pod{Name: "new", UID: "u2"}, t0.Add(time.Minute), false)

	for _, tt := range []struct {
		at   time.Duration
		want string
	}{{10 * time.Second, "old"}, {90 * time.Second, "new"}} {
		p, ok := o.PodForIP(ip, t0.Add(tt.at))
		if !ok || p.Name != tt.want {
			t.Errorf("at +%s: got %q (%v), want %q", tt.at, p.Name, ok, tt.want)
		}
	}
	if _, ok := o.PodForIP(ip, t0.Add(45*time.Second)); ok {
		t.Error("nobody held the address between the two pods")
	}
}

func TestImagePullFailureIsRecordedOncePerPodAndImage(t *testing.T) {
	o := New(fake.NewClientset(), t.Logf)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-cleanup-abc", Namespace: "demo", UID: "u1"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "cleanup", Image: "busybox:1.37.0"}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "cleanup",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull", Message: "not found"}},
		}}},
	}
	ref := evidence.Pod{Name: pod.Name, Namespace: "demo", UID: "u1", Owner: "CronJob demo-cleanup"}
	o.checkContainers(context.Background(), pod, ref, time.Now())
	pod.Status.ContainerStatuses[0].State.Waiting.Reason = "ImagePullBackOff"
	o.checkContainers(context.Background(), pod, ref, time.Now())

	ev := o.Evidence()
	if len(ev) != 1 || ev[0].Kind != evidence.ImagePullFailed || ev[0].Image != "busybox:1.37.0" || ev[0].Pod.Owner != "CronJob demo-cleanup" {
		t.Fatalf("want one image-pull-failed item for busybox:1.37.0, got %+v", ev)
	}
}
