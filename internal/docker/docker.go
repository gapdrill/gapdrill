// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package docker is a thin wrapper around the docker CLI. kind drives the
// same CLI, so gapdrill needs nothing else installed.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
)

// Client runs docker commands.
type Client struct {
	bin string
}

// New finds the docker CLI and checks that the daemon answers.
func New(ctx context.Context) (*Client, error) {
	bin, err := exec.LookPath("docker")
	if err != nil {
		return nil, errors.New("docker CLI not found on PATH; gapdrill needs Docker (or Docker Desktop) to run kind clusters")
	}
	c := &Client{bin: bin}
	if _, err := c.Run(ctx, "info", "--format", "{{.ServerVersion}}"); err != nil {
		return nil, fmt.Errorf("the Docker daemon isn't answering (is Docker running?): %w", err)
	}
	return c, nil
}

// Run runs docker with args and returns its trimmed standard output.
func (c *Client) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("docker %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Architecture returns the daemon's architecture in OCI terms ("amd64",
// "arm64"), which is also the kind nodes' architecture.
func (c *Client) Architecture(ctx context.Context) (string, error) {
	out, err := c.Run(ctx, "info", "--format", "{{.Architecture}}")
	if err != nil {
		return "", err
	}
	switch out {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return out, nil
	}
}

// Subnet is one IPAM entry of a Docker network.
type Subnet struct {
	Prefix  netip.Prefix
	Gateway netip.Addr
}

// NetworkSubnets returns the subnets and gateways of network.
func (c *Client) NetworkSubnets(ctx context.Context, network string) ([]Subnet, error) {
	out, err := c.Run(ctx, "network", "inspect", "--format", "{{json .IPAM.Config}}", network)
	if err != nil {
		return nil, err
	}
	var cfg []struct {
		Subnet  string `json:"Subnet"`
		Gateway string `json:"Gateway"`
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return nil, fmt.Errorf("parse IPAM config of network %s: %w", network, err)
	}
	var subnets []Subnet
	for _, e := range cfg {
		p, err := netip.ParsePrefix(e.Subnet)
		if err != nil {
			continue
		}
		s := Subnet{Prefix: p}
		if gw, err := netip.ParseAddr(e.Gateway); err == nil {
			s.Gateway = gw
		}
		subnets = append(subnets, s)
	}
	if len(subnets) == 0 {
		return nil, fmt.Errorf("network %s has no subnets", network)
	}
	return subnets, nil
}
