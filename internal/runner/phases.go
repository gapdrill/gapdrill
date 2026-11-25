// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/gapdrill/gapdrill/internal/cluster"
	"github.com/gapdrill/gapdrill/internal/dnsmon"
	"github.com/gapdrill/gapdrill/internal/evidence"
	"github.com/gapdrill/gapdrill/internal/learn"
	"github.com/gapdrill/gapdrill/internal/lockdown"
	"github.com/gapdrill/gapdrill/internal/mirror"
	"github.com/gapdrill/gapdrill/internal/observe"
	"github.com/gapdrill/gapdrill/internal/ops"
	"github.com/gapdrill/gapdrill/internal/registry"
	"github.com/gapdrill/gapdrill/internal/report"
	"github.com/gapdrill/gapdrill/internal/spec"
)

// prepare starts the bundle registry and copies the bundle into it while the
// cluster is created, since the two don't depend on each other.
func (r *run) prepare(ctx context.Context) error {
	var wg sync.WaitGroup
	var bundleErr, clusterErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		bundleErr = r.prepareBundle(ctx)
	}()
	go func() {
		defer wg.Done()
		r.cluster, clusterErr = cluster.Create(cluster.Options{
			Name:           r.name,
			Network:        r.name,
			NodeImage:      r.drill.Cluster.NodeImage,
			KubeconfigPath: filepath.Join(r.tmpDir, "kubeconfig"),
		})
	}()
	wg.Wait()
	if err := errors.Join(bundleErr, clusterErr); err != nil {
		return err
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", r.cluster.KubeconfigPath)
	if err != nil {
		return err
	}
	cfg.QPS, cfg.Burst = 50, 100
	r.cs, err = kubernetes.NewForConfig(cfg)
	return err
}

func (r *run) prepareBundle(ctx context.Context) error {
	reg, err := registry.Start(ctx, r.docker, r.name+"-registry", r.name, map[string]string{runLabel: r.id})
	if err != nil {
		return err
	}
	r.registry = reg
	arch := strings.TrimPrefix(r.env.Platform, "linux/")
	r.bundleDigests = make(map[string]string, len(r.images))
	for _, img := range r.images {
		r.p.detail("copying %s", img)
		digest, err := reg.Copy(ctx, img, arch)
		if err != nil {
			return err
		}
		r.bundleDigests[img.String()] = digest
	}
	return nil
}

// isolate points containerd at the bundle registry, makes CoreDNS answer
// like a disconnected site and installs the egress rules.
func (r *run) isolate(ctx context.Context) error {
	nodes := r.cluster.Nodes()

	files := mirror.HostsFiles(r.registry.NodeAddr, r.registries())
	if r.o.Learn {
		files = mirror.LearnHostsFiles(r.registry.NodeAddr, r.registries())
	}
	for _, node := range nodes {
		for rel, content := range files {
			if err := r.cluster.WriteFile(ctx, node, path.Join(mirror.HostsDir, rel), content); err != nil {
				return err
			}
		}
	}
	if r.o.Learn {
		r.p.detail("containerd on %d node%s asks the bundle registry at %s first, then the image's own registry", len(nodes), plural(len(nodes)), r.registry.NodeAddr)
	} else {
		r.p.detail("containerd on %d node%s pulls only from the bundle registry at %s", len(nodes), plural(len(nodes)), r.registry.NodeAddr)
	}

	if out, err := r.cluster.Exec(ctx, nodes[0], "cat", "/etc/resolv.conf"); err == nil {
		r.searchDomains = searchDomains(out)
	}

	subnets, err := r.docker.NetworkSubnets(ctx, r.name)
	if err != nil {
		return err
	}
	network := lockdown.Network{
		PodCIDR:     netip.MustParsePrefix(cluster.PodCIDR),
		ServiceCIDR: netip.MustParsePrefix(cluster.ServiceCIDR),
		NodesOnline: r.o.Learn,
	}
	for _, s := range subnets {
		network.NodeSubnets = append(network.NodeSubnets, s.Prefix)
		if s.Gateway.IsValid() {
			network.Gateways = append(network.Gateways, s.Gateway)
		}
	}

	r.iso.ProbeURL = r.o.ProbeURL
	r.iso.ReachableBefore = lockdown.Reachable(ctx, r.cluster, nodes[0], r.o.ProbeURL)
	for _, node := range nodes {
		warnings, err := lockdown.Apply(ctx, r.cluster, node, network)
		r.iso.Warnings = append(r.iso.Warnings, warnings...)
		if err != nil {
			return fmt.Errorf("isolation failed: %w", err)
		}
	}
	r.p.detail("egress rules installed on %d node%s", len(nodes), plural(len(nodes)))

	if err := r.offlineDNS(ctx); err != nil {
		return err
	}
	r.isolatedAt = time.Now()
	r.p.detail("CoreDNS answers NXDOMAIN for names outside the cluster")
	return nil
}

// offlineDNS replaces the CoreDNS configuration and restarts CoreDNS, so the
// new configuration is live before any operation runs.
func (r *run) offlineDNS(ctx context.Context) error {
	cms := r.cs.CoreV1().ConfigMaps("kube-system")
	cm, err := cms.Get(ctx, "coredns", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read the CoreDNS configuration: %w", err)
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["Corefile"] = dnsmon.Corefile()
	if _, err := cms.Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update the CoreDNS configuration: %w", err)
	}
	pods := r.cs.CoreV1().Pods("kube-system")
	selector := metav1.ListOptions{LabelSelector: "k8s-app=kube-dns"}
	if err := pods.DeleteCollection(ctx, metav1.DeleteOptions{}, selector); err != nil {
		return fmt.Errorf("restart CoreDNS: %w", err)
	}
	err = wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		d, err := r.cs.AppsV1().Deployments("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
		if err != nil || d.Spec.Replicas == nil {
			return false, nil
		}
		list, err := pods.List(ctx, selector)
		if err != nil {
			return false, nil
		}
		ready := 0
		for i := range list.Items {
			p := &list.Items[i]
			if p.DeletionTimestamp != nil {
				return false, nil // an old pod is still shutting down
			}
			if podReady(p) {
				ready++
			}
		}
		want := int(*d.Spec.Replicas)
		return ready == want && len(list.Items) == want, nil
	})
	if err != nil {
		return fmt.Errorf("CoreDNS didn't become ready with the offline configuration: %w", err)
	}
	return nil
}

// verify proves the nodes can no longer reach the probe URL.
func (r *run) verify(ctx context.Context) error {
	if r.o.Learn {
		// The nodes are online on purpose, so containerd can pull what the
		// bundle lacks. The pods' rules were checked when they were applied.
		r.iso.Learn = true
		r.p.detail("learn run: the nodes stay online for image pulls; the pods are cut off")
		return nil
	}
	for _, node := range r.cluster.Nodes() {
		if lockdown.Reachable(ctx, r.cluster, node, r.o.ProbeURL) {
			return fmt.Errorf("isolation failed: %s is still reachable from node %s", r.o.ProbeURL, node)
		}
	}
	r.iso.Verified = true
	if r.iso.ReachableBefore {
		r.p.detail("%s was reachable before isolation and isn't now", r.o.ProbeURL)
	} else {
		r.p.warnf("%s wasn't reachable before isolation either, so the check proves less than usual (is the host offline?)", r.o.ProbeURL)
	}
	return nil
}

func (r *run) operations(ctx context.Context) error {
	r.obs = observe.New(r.cs, r.p.debugf)
	if err := r.obs.Start(ctx); err != nil {
		return fmt.Errorf("watch the cluster: %w", err)
	}
	if err := r.obs.FollowDNS(ctx, r.isolatedAt); err != nil {
		return fmt.Errorf("follow CoreDNS logs: %w", err)
	}

	if r.o.Learn {
		images, err := r.nodeImages(ctx)
		if err != nil {
			r.p.warnf("can't list the nodes' images, so this learn run won't list what the release pulled: %v", err)
		}
		r.imagesBefore = images
	}

	env := ops.Env{
		Kubeconfig:     r.cluster.KubeconfigPath,
		HelmBin:        r.helmBin,
		ChartRef:       r.drill.ChartRef(),
		Release:        r.drill.Release.Name,
		Namespace:      r.drill.Release.Namespace,
		ValuesFiles:    r.drill.ValuesFiles(),
		Set:            r.drill.Release.Set,
		Dir:            r.drill.Dir,
		Clientset:      r.cs,
		SkipNamespaces: observe.SystemNamespaces,
	}
	for _, declared := range r.drill.Operations {
		// Some operations depend on what earlier ones created, such as the
		// CronJobs an install made, so they're expanded only now.
		expanded, skip, err := ops.Expand(ctx, env, declared)
		switch {
		case err != nil:
			now := time.Now()
			r.record(declared, now, now, ops.Result{Err: err})
			continue
		case skip != "":
			now := time.Now()
			r.runs = append(r.runs, report.OperationRun{
				Name: declared.Name, Type: declared.Type(), Start: now, End: now, Skipped: skip,
			})
			r.p.skipped(declared.Name, skip)
			continue
		}
		for _, op := range expanded {
			start := time.Now()
			res := ops.Run(ctx, env, op)
			r.record(op, start, time.Now(), res)
			// Let late effects of this operation land in its window before the
			// next one starts.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(op.SettlePeriod()):
			}
		}
	}
	return nil
}

func (r *run) record(op spec.Operation, start, end time.Time, res ops.Result) {
	r.runs = append(r.runs, report.OperationRun{
		Name: op.Name, Type: op.Type(), Start: start, End: end,
		Settle: op.SettlePeriod(), Err: res.Err, Output: res.Output,
	})
	r.p.operation(op.Name, res.Err, end.Sub(start))
}

// collect gathers the evidence that is only available at the end, then stops
// the observer. It is best effort: the drill may have failed part-way.
func (r *run) collect() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if r.obs != nil && r.registry != nil {
		if logs, err := r.registry.Logs(ctx, r.docker); err == nil {
			since := r.isolatedAt.Add(-time.Second).Truncate(time.Second)
			var misses []registry.Request
			for _, req := range registry.ParseAccessLog(logs) {
				if !req.FromContainerd() || req.Time.Before(since) {
					continue
				}
				switch {
				case req.Status == 404:
					misses = append(misses, req)
				case req.Status >= 200 && req.Status < 300:
					r.pulled = append(r.pulled, req.UpstreamReference(r.registries()))
				}
			}
			for _, req := range registry.WithoutDigestFollowUps(misses) {
				r.obs.Add(evidence.Item{Kind: evidence.ManifestNotFound, Time: req.Time, Image: req.UpstreamReference(r.registries())})
			}
		} else {
			r.p.debugf("registry logs: %v", err)
		}
	}
	if r.obs != nil {
		r.obs.Stop()
	}
	if r.cluster != nil && !r.isolatedAt.IsZero() {
		for _, node := range r.cluster.Nodes() {
			r.iso.RejectedPackets += lockdown.Rejected(ctx, r.cluster, node)
		}
	}
	if r.o.Learn && r.imagesBefore != nil {
		if after, err := r.nodeImages(ctx); err == nil {
			r.learned = learn.New(r.imagesBefore, after)
		} else {
			r.p.warnf("can't list the nodes' images, so this learn run won't list what the release pulled: %v", err)
		}
	}
}

// nodeImages lists the images on every node, as canonical references.
func (r *run) nodeImages(ctx context.Context) ([]string, error) {
	var refs []string
	for _, node := range r.cluster.Nodes() {
		out, err := r.cluster.Exec(ctx, node, learn.ImagesCommand...)
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", node, err)
		}
		images, err := learn.ParseImages(out)
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", node, err)
		}
		refs = append(refs, images...)
	}
	return refs, nil
}

// attributedEvidence ties each DNS query to the pod that held the client IP
// at the time, and drops evidence from the cluster's own components.
func (r *run) attributedEvidence() []evidence.Item {
	system := map[string]bool{}
	for _, ns := range observe.SystemNamespaces {
		system[ns] = true
	}
	var out []evidence.Item
	for _, e := range r.obs.Evidence() {
		if e.Kind == evidence.DNSQuery {
			if p, ok := r.obs.PodForIP(e.Client, e.Time); ok {
				if system[p.Namespace] {
					continue
				}
				e.Pod = &p
			}
		}
		out = append(out, e)
	}
	return out
}

func (r *run) registries() []string {
	seen := map[string]bool{}
	var out []string
	for _, img := range r.images {
		if !seen[img.Registry] {
			seen[img.Registry] = true
			out = append(out, img.Registry)
		}
	}
	return out
}

func searchDomains(resolvConf string) []string {
	for _, line := range strings.Split(resolvConf, "\n") {
		if f := strings.Fields(line); len(f) > 1 && f[0] == "search" {
			return f[1:]
		}
	}
	return nil
}

func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
