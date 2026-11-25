// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package registry runs the drill's bundle registry: an OCI registry holding
// only the bundle's images, reachable from the kind nodes.
package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/gapdrill/gapdrill/internal/docker"
	"github.com/gapdrill/gapdrill/internal/mirror"
)

// Image is the registry image: CNCF Distribution v3, pinned by digest.
const Image = "docker.io/library/registry:3@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c"

const port = "5000"

// Registry is a running bundle registry.
type Registry struct {
	// Container is the registry's container name.
	Container string
	// HostAddr is where the host pushes to, on 127.0.0.1 only.
	HostAddr string
	// NodeAddr is where the kind nodes pull from, on the drill's network.
	NodeAddr string
}

// Start runs the registry on network, publishing it on 127.0.0.1 only, and
// waits until it answers.
func Start(ctx context.Context, d *docker.Client, container, network string, labels map[string]string) (*Registry, error) {
	args := []string{"run", "--detach", "--name", container, "--network", network, "--publish", "127.0.0.1::" + port}
	for k, v := range labels {
		args = append(args, "--label", k+"="+v)
	}
	if _, err := d.Run(ctx, append(args, Image)...); err != nil {
		return nil, fmt.Errorf("start bundle registry: %w", err)
	}

	hostPort, err := d.Run(ctx, "port", container, port+"/tcp")
	if err != nil {
		return nil, err
	}
	hostAddr := firstLine(hostPort)
	ip, err := d.Run(ctx, "inspect", "--format", fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", network), container)
	if err != nil {
		return nil, err
	}
	if ip == "" {
		return nil, fmt.Errorf("bundle registry has no address on network %s", network)
	}

	r := &Registry{Container: container, HostAddr: hostAddr, NodeAddr: ip + ":" + port}
	if err := r.waitReady(ctx, 30*time.Second); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) waitReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	url := "http://" + r.HostAddr + "/v2/"
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("bundle registry at %s didn't become ready: %w", url, ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Copy copies one bundle image from its upstream registry into the bundle
// registry. For tag references only the nodes' platform is copied, which
// keeps runs fast; digest references are copied whole so their digest stays
// valid.
//
// It uses the Docker credential chain and retries anonymously when the
// registry rejects the credentials, since stale logins are common and public
// bundles shouldn't fail because of them.
func (r *Registry) Copy(ctx context.Context, img mirror.Image, arch string) error {
	src := img.String()
	dst := img.MirrorReference(r.HostAddr)
	opts := []crane.Option{crane.WithContext(ctx), crane.WithUserAgent("gapdrill")}
	if img.Digest == "" {
		opts = append(opts, crane.WithPlatform(&v1.Platform{OS: "linux", Architecture: arch}))
	}
	err := crane.Copy(src, dst, append(opts, crane.WithAuthFromKeychain(authn.DefaultKeychain))...)
	if isAuthError(err) {
		err = crane.Copy(src, dst, append(opts, crane.WithAuth(authn.Anonymous))...)
	}
	if err != nil {
		return fmt.Errorf("copy %s into the bundle registry: %w", src, err)
	}
	return nil
}

// Logs returns the registry's log output.
func (r *Registry) Logs(ctx context.Context, d *docker.Client) (string, error) {
	return d.Run(ctx, "logs", r.Container)
}

func isAuthError(err error) bool {
	var terr *transport.Error
	return errors.As(err, &terr) && (terr.StatusCode == http.StatusUnauthorized || terr.StatusCode == http.StatusForbidden)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
