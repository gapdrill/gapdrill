// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package cluster creates and drives the drill's kind cluster.
package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/nodes"
)

// Networking defaults. They are set explicitly so the egress rules can name
// them.
const (
	PodCIDR     = "10.244.0.0/16"
	ServiceCIDR = "10.96.0.0/16"
)

// containerdPatch points containerd at the per-registry hosts.toml files that
// gapdrill writes during isolation. kind node images don't set it by default.
const containerdPatch = `[plugins."io.containerd.grpc.v1.cri".registry]
  config_path = "/etc/containerd/certs.d"`

// Options configure a new cluster.
type Options struct {
	Name string
	// Network is an existing Docker network for the nodes.
	Network string
	// NodeImage overrides kind's default node image.
	NodeImage string
	// KubeconfigPath is where the cluster's kubeconfig is written. kind would
	// otherwise edit the user's ~/.kube/config.
	KubeconfigPath string
}

// Cluster is a running kind cluster.
type Cluster struct {
	Name           string
	KubeconfigPath string
	provider       *kindcluster.Provider
	nodes          []nodes.Node
}

// Create creates a single-node cluster on the given network and waits for the
// control plane to be ready.
func Create(o Options) (*Cluster, error) {
	// kind reads the network name from the environment. gapdrill creates one
	// cluster per process, so setting it here is safe.
	if err := os.Setenv("KIND_EXPERIMENTAL_DOCKER_NETWORK", o.Network); err != nil {
		return nil, err
	}
	noSearch := []string{}
	cfg := &v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{Kind: "Cluster", APIVersion: "kind.x-k8s.io/v1alpha4"},
		Nodes:    []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}},
		Networking: v1alpha4.Networking{
			IPFamily:      v1alpha4.IPv4Family,
			PodSubnet:     PodCIDR,
			ServiceSubnet: ServiceCIDR,
			// No search domains from the host: pods' lookups stay as written,
			// which keeps the DNS evidence readable.
			DNSSearch: &noSearch,
		},
		ContainerdConfigPatches: []string{containerdPatch},
	}
	opts := []kindcluster.CreateOption{
		kindcluster.CreateWithV1Alpha4Config(cfg),
		kindcluster.CreateWithKubeconfigPath(o.KubeconfigPath),
		kindcluster.CreateWithWaitForReady(3 * time.Minute),
		kindcluster.CreateWithDisplayUsage(false),
		kindcluster.CreateWithDisplaySalutation(false),
	}
	if o.NodeImage != "" {
		opts = append(opts, kindcluster.CreateWithNodeImage(o.NodeImage))
	}

	p := kindcluster.NewProvider(kindcluster.ProviderWithDocker())
	c := &Cluster{Name: o.Name, KubeconfigPath: o.KubeconfigPath, provider: p}
	if err := p.Create(o.Name, opts...); err != nil {
		return c, fmt.Errorf("create kind cluster %s: %w", o.Name, err)
	}
	ns, err := p.ListNodes(o.Name)
	if err != nil {
		return c, err
	}
	c.nodes = ns
	return c, nil
}

// Delete deletes the cluster's nodes. It only touches the cluster's own
// kubeconfig file.
func (c *Cluster) Delete() error {
	return c.provider.Delete(c.Name, c.KubeconfigPath)
}

// Nodes returns the node names.
func (c *Cluster) Nodes() []string {
	names := make([]string, len(c.nodes))
	for i, n := range c.nodes {
		names[i] = n.String()
	}
	return names
}

// Exec runs a command on a node and returns its standard output.
func (c *Cluster) Exec(ctx context.Context, node string, args ...string) (string, error) {
	n, err := c.node(node)
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	if err := n.CommandContext(ctx, args[0], args[1:]...).SetStdout(&stdout).SetStderr(&stderr).Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("%s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

// WriteFile writes content to path on a node, creating parent directories.
func (c *Cluster) WriteFile(ctx context.Context, node, filePath, content string) error {
	n, err := c.node(node)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd := n.CommandContext(ctx, "sh", "-c", `mkdir -p "$1" && cat > "$2"`, "sh", path.Dir(filePath), filePath)
	if err := cmd.SetStdin(strings.NewReader(content)).SetStderr(&stderr).Run(); err != nil {
		return fmt.Errorf("write %s on %s: %s", filePath, node, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (c *Cluster) node(name string) (nodes.Node, error) {
	for _, n := range c.nodes {
		if n.String() == name {
			return n, nil
		}
	}
	return nil, fmt.Errorf("no node %q in cluster %s", name, c.Name)
}

// List returns the names of existing kind clusters whose names start with
// prefix.
func List(prefix string) ([]string, error) {
	all, err := kindcluster.NewProvider(kindcluster.ProviderWithDocker()).List()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range all {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out, nil
}

// DeleteByName deletes a kind cluster by name. kubeconfigPath is passed so
// that kind doesn't edit the user's kubeconfig; it may not exist.
func DeleteByName(name, kubeconfigPath string) error {
	return kindcluster.NewProvider(kindcluster.ProviderWithDocker()).Delete(name, kubeconfigPath)
}
