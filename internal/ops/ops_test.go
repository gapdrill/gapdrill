// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package ops

import (
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHelmInstallArgsAreDeterministic(t *testing.T) {
	env := Env{
		Kubeconfig: "/tmp/kc", ChartRef: "/drill/chart", Release: "demo", Namespace: "demo",
		ValuesFiles: []string{"/drill/a.yaml", "/drill/b.yaml"},
		Set:         map[string]string{"z": "1", "a": "2"},
	}
	got := strings.Join(HelmInstallArgs(env, 3*time.Minute), " ")
	want := "upgrade --install demo /drill/chart --namespace demo --create-namespace --wait --timeout 3m0s --kubeconfig /tmp/kc --values /drill/a.yaml --values /drill/b.yaml --set a=2 --set z=1"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestJobFromCronJob(t *testing.T) {
	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: strings.Repeat("a", 60), Namespace: "demo", UID: "cj-uid"},
		Spec: batchv1.CronJobSpec{JobTemplate: batchv1.JobTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "demo"}, Annotations: map[string]string{"team": "x"}},
		}},
	}
	j := JobFromCronJob(cj, time.Unix(1764000000, 0))
	if len(j.Name) > 63 || !strings.Contains(j.Name, "-gapdrill-") {
		t.Errorf("name %q must fit 63 characters and keep the gapdrill suffix", j.Name)
	}
	if j.Annotations["cronjob.kubernetes.io/instantiate"] != "manual" || j.Annotations["team"] != "x" {
		t.Errorf("annotations = %v", j.Annotations)
	}
	if ref := metav1.GetControllerOf(j); ref == nil || ref.Kind != "CronJob" || ref.UID != "cj-uid" {
		t.Errorf("the CronJob must own the Job, got %+v", ref)
	}
}

func TestErrorLinePrefersHelmErrors(t *testing.T) {
	out := "Release \"demo\" does not exist. Installing it now.\nError: INSTALLATION FAILED: failed post-install: 1 error occurred:\n\t* job demo-post-install failed: BackoffLimitExceeded\n"
	if got := errorLine(out, nil); got != "INSTALLATION FAILED: failed post-install: 1 error occurred: * job demo-post-install failed: BackoffLimitExceeded" {
		t.Errorf("got %q", got)
	}
}

func TestTail(t *testing.T) {
	if got := Tail("1\n2\n3\n4\n", 2); got != "3\n4" {
		t.Errorf("got %q", got)
	}
}
