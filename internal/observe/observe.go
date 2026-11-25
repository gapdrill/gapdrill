// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package observe records evidence while a drill runs: image pull failures,
// failing containers and their logs, and DNS queries for outside names.
package observe

import (
	"bufio"
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/gapdrill/gapdrill/internal/dnsmon"
	"github.com/gapdrill/gapdrill/internal/evidence"
)

// SystemNamespaces hold the cluster's own components. Their pods are tracked
// (for DNS attribution) but produce no findings.
var SystemNamespaces = []string{"kube-system", "kube-public", "kube-node-lease", "local-path-storage"}

var pullFailures = map[string]bool{
	"ErrImagePull":      true,
	"ImagePullBackOff":  true,
	"InvalidImageName":  true,
	"ErrImageNeverPull": true,
}

// Observer watches the cluster and records evidence.
type Observer struct {
	cs     kubernetes.Interface
	ignore map[string]bool
	logf   func(format string, args ...any)

	mu     sync.Mutex
	items  []evidence.Item
	spans  map[netip.Addr][]span
	seen   map[string]bool
	owners map[types.UID]string

	factory informers.SharedInformerFactory
	jobs    batchlisters.JobLister
	rs      appslisters.ReplicaSetLister
	cancel  context.CancelFunc
	stop    chan struct{}
	logs    sync.WaitGroup // container log fetches in flight
	streams sync.WaitGroup // CoreDNS log streams
}

// span records that a pod held an IP address from one time to another.
type span struct {
	pod      evidence.Pod
	from, to time.Time // to is zero while the pod exists
}

// New returns an observer. logf receives diagnostic messages.
func New(cs kubernetes.Interface, logf func(format string, args ...any)) *Observer {
	ignore := make(map[string]bool, len(SystemNamespaces))
	for _, ns := range SystemNamespaces {
		ignore[ns] = true
	}
	return &Observer{
		cs:     cs,
		ignore: ignore,
		logf:   logf,
		spans:  map[netip.Addr][]span{},
		seen:   map[string]bool{},
		owners: map[types.UID]string{},
	}
}

// Start begins watching pods. It returns once the initial list is in.
func (o *Observer) Start(ctx context.Context) error {
	ctx, o.cancel = context.WithCancel(ctx)
	o.factory = informers.NewSharedInformerFactory(o.cs, 0)
	o.jobs = o.factory.Batch().V1().Jobs().Lister()
	o.rs = o.factory.Apps().V1().ReplicaSets().Lister()
	pods := o.factory.Core().V1().Pods().Informer()
	if _, err := pods.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { o.onPod(ctx, obj, false) },
		UpdateFunc: func(_, obj any) { o.onPod(ctx, obj, false) },
		DeleteFunc: func(obj any) { o.onPod(ctx, obj, true) },
	}); err != nil {
		return err
	}
	o.stop = make(chan struct{})
	o.factory.Start(o.stop)
	for typ, ok := range o.factory.WaitForCacheSync(o.stop) {
		if !ok {
			return fmt.Errorf("watch %v: cache didn't sync", typ)
		}
	}
	return nil
}

// FollowDNS streams the query logs of the running CoreDNS pods from since
// onwards.
func (o *Observer) FollowDNS(ctx context.Context, since time.Time) error {
	list, err := o.cs.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{LabelSelector: "k8s-app=kube-dns"})
	if err != nil {
		return err
	}
	n := 0
	for _, p := range list.Items {
		if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning {
			continue
		}
		n++
		o.streams.Add(1)
		go o.follow(ctx, p.Name, since)
	}
	if n == 0 {
		return fmt.Errorf("no running CoreDNS pods to follow")
	}
	return nil
}

func (o *Observer) follow(ctx context.Context, pod string, since time.Time) {
	defer o.streams.Done()
	st := metav1.NewTime(since)
	rc, err := o.cs.CoreV1().Pods("kube-system").GetLogs(pod, &corev1.PodLogOptions{Follow: true, Timestamps: true, SinceTime: &st}).Stream(ctx)
	if err != nil {
		o.logf("follow CoreDNS logs of %s: %v", pod, err)
		return
	}
	defer rc.Close()
	s := bufio.NewScanner(rc)
	for s.Scan() {
		if q, ok := dnsmon.ParseLine(s.Text()); ok {
			o.add(evidence.Item{Kind: evidence.DNSQuery, Time: q.Time, Client: q.Client, QName: q.Name, QType: q.Type, Reason: q.RCode})
		}
	}
}

// Stop stops watching, waits for pending log fetches, then ends the DNS log
// streams.
func (o *Observer) Stop() {
	// Shut the informers down first. Shutdown returns once their handlers
	// have finished, so no new log fetch can start (and call logs.Add) while
	// logs.Wait is running.
	if o.stop != nil {
		close(o.stop)
		o.factory.Shutdown()
	}
	o.logs.Wait()
	if o.cancel != nil {
		o.cancel()
	}
	o.streams.Wait()
}

// Add records evidence gathered elsewhere, such as registry requests.
func (o *Observer) Add(items ...evidence.Item) {
	for _, it := range items {
		o.add(it)
	}
}

// Evidence returns everything recorded so far, oldest first.
func (o *Observer) Evidence() []evidence.Item {
	o.mu.Lock()
	out := append([]evidence.Item(nil), o.items...)
	o.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// PodForIP returns the pod that held ip at time t.
func (o *Observer) PodForIP(ip netip.Addr, t time.Time) (evidence.Pod, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	// Log timestamps come from the node's clock and pod times from the API
	// server's; allow a little slack between them.
	const slack = 2 * time.Second
	var best *span
	for i := range o.spans[ip] {
		s := &o.spans[ip][i]
		if t.Before(s.from.Add(-slack)) || (!s.to.IsZero() && t.After(s.to.Add(slack))) {
			continue
		}
		if best == nil || s.from.After(best.from) {
			best = s
		}
	}
	if best == nil {
		return evidence.Pod{}, false
	}
	return best.pod, true
}

func (o *Observer) add(it evidence.Item) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.items = append(o.items, it)
	return len(o.items) - 1
}

func (o *Observer) firstTime(key string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seen[key] {
		return false
	}
	o.seen[key] = true
	return true
}

func (o *Observer) onPod(ctx context.Context, obj any, deleted bool) {
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = d.Obj
	}
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	now := time.Now()
	ref := o.podRef(ctx, pod)
	o.trackIPs(pod, ref, now, deleted)
	if deleted || o.ignore[pod.Namespace] {
		return
	}
	o.checkContainers(ctx, pod, ref, now)
}

func (o *Observer) trackIPs(pod *corev1.Pod, ref evidence.Pod, now time.Time, deleted bool) {
	if pod.Spec.HostNetwork {
		return // shares the node's IP; its queries can't be told apart
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, pip := range pod.Status.PodIPs {
		ip, err := netip.ParseAddr(pip.IP)
		if err != nil {
			continue
		}
		spans := o.spans[ip]
		known := false
		for i := range spans {
			if spans[i].pod.UID == ref.UID {
				known = true
				if deleted && spans[i].to.IsZero() {
					spans[i].to = now
				}
			}
		}
		if !known && !deleted {
			from := pod.CreationTimestamp.Time
			if from.IsZero() {
				from = now
			}
			o.spans[ip] = append(spans, span{pod: ref, from: from})
		}
	}
}

func (o *Observer) checkContainers(ctx context.Context, pod *corev1.Pod, ref evidence.Pod, now time.Time) {
	images := map[string]string{}
	for _, c := range pod.Spec.InitContainers {
		images[c.Name] = c.Image
	}
	for _, c := range pod.Spec.Containers {
		images[c.Name] = c.Image
	}
	statuses := append(append([]corev1.ContainerStatus(nil), pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for _, cs := range statuses {
		if w := cs.State.Waiting; w != nil && pullFailures[w.Reason] {
			image := images[cs.Name]
			if image == "" {
				image = cs.Image
			}
			if o.firstTime("pull/" + string(pod.UID) + "/" + image) {
				r := ref
				o.add(evidence.Item{Kind: evidence.ImagePullFailed, Time: now, Pod: &r, Image: image, Container: cs.Name, Reason: w.Reason, Message: w.Message})
			}
		}
		for i, term := range []*corev1.ContainerStateTerminated{cs.State.Terminated, cs.LastTerminationState.Terminated} {
			if term == nil || term.ExitCode == 0 {
				continue
			}
			if !o.firstTime(fmt.Sprintf("exit/%s/%s/%s/%d", pod.UID, cs.Name, term.ContainerID, term.FinishedAt.Unix())) {
				continue
			}
			at := term.FinishedAt.Time
			if at.IsZero() {
				at = now
			}
			r := ref
			idx := o.add(evidence.Item{Kind: evidence.ContainerFailed, Time: at, Pod: &r, Container: cs.Name, ExitCode: term.ExitCode, Reason: term.Reason, Message: term.Message})
			o.fetchLogs(ctx, pod.Namespace, pod.Name, cs.Name, i == 1, idx)
		}
	}
}

func (o *Observer) fetchLogs(ctx context.Context, ns, pod, container string, previous bool, idx int) {
	o.logs.Add(1)
	go func() {
		defer o.logs.Done()
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		tail := int64(20)
		b, err := o.cs.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{Container: container, Previous: previous, TailLines: &tail}).DoRaw(ctx)
		if err != nil {
			o.logf("logs of %s/%s (%s): %v", ns, pod, container, err)
			return
		}
		text := strings.TrimRight(string(b), "\n")
		if text == "" {
			return
		}
		o.mu.Lock()
		o.items[idx].Logs = strings.Split(text, "\n")
		o.mu.Unlock()
	}()
}

func (o *Observer) podRef(ctx context.Context, pod *corev1.Pod) evidence.Pod {
	o.mu.Lock()
	owner, ok := o.owners[pod.UID]
	o.mu.Unlock()
	if !ok {
		owner = o.describeOwner(ctx, pod)
		o.mu.Lock()
		o.owners[pod.UID] = owner
		o.mu.Unlock()
	}
	return evidence.Pod{Namespace: pod.Namespace, Name: pod.Name, UID: string(pod.UID), Owner: owner}
}

// describeOwner names what a user would recognise: the CronJob rather than
// its Job, the Deployment rather than its ReplicaSet, and Helm hooks as such.
func (o *Observer) describeOwner(ctx context.Context, pod *corev1.Pod) string {
	ref := metav1.GetControllerOf(pod)
	if ref == nil {
		if hook := pod.Annotations["helm.sh/hook"]; hook != "" {
			return fmt.Sprintf("Pod %s (Helm %s hook)", pod.Name, hook)
		}
		return "Pod " + pod.Name
	}
	switch ref.Kind {
	case "Job":
		job, err := o.job(ctx, pod.Namespace, ref.Name)
		if err != nil {
			return "Job " + ref.Name
		}
		if hook := job.Annotations["helm.sh/hook"]; hook != "" {
			return fmt.Sprintf("Job %s (Helm %s hook)", job.Name, hook)
		}
		if cj := metav1.GetControllerOf(job); cj != nil && cj.Kind == "CronJob" {
			return "CronJob " + cj.Name
		}
		return "Job " + job.Name
	case "ReplicaSet":
		if rs, err := o.replicaSet(ctx, pod.Namespace, ref.Name); err == nil {
			if d := metav1.GetControllerOf(rs); d != nil {
				return d.Kind + " " + d.Name
			}
		}
		return "ReplicaSet " + ref.Name
	default:
		return ref.Kind + " " + ref.Name
	}
}

// job reads from the informer cache, falling back to the API for objects
// the cache hasn't seen yet.
func (o *Observer) job(ctx context.Context, ns, name string) (*batchv1.Job, error) {
	if o.jobs != nil {
		if j, err := o.jobs.Jobs(ns).Get(name); err == nil {
			return j, nil
		}
	}
	return o.cs.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
}

func (o *Observer) replicaSet(ctx context.Context, ns, name string) (metav1.Object, error) {
	if o.rs != nil {
		if rs, err := o.rs.ReplicaSets(ns).Get(name); err == nil {
			return rs, nil
		}
	}
	return o.cs.AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
}
