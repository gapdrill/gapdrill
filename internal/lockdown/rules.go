// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package lockdown cuts kind nodes off from everything outside the cluster,
// using iptables and ip6tables rules inside each node.
package lockdown

import (
	"bufio"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Chain is the iptables chain gapdrill installs in each node.
const Chain = "GAPDRILL-EGRESS"

// Network describes what the drill's nodes may still reach.
type Network struct {
	// NodeSubnets are the subnets of the drill's Docker network. The nodes
	// and the bundle registry live there.
	NodeSubnets []netip.Prefix
	// Gateways are the Docker network's gateways. On Linux the gateway is the
	// host itself, which may run a proxy with internet access, so new
	// connections to it are rejected.
	Gateways    []netip.Addr
	PodCIDR     netip.Prefix
	ServiceCIDR netip.Prefix
}

// Family is an IP family's iptables binary and its specifics.
type Family struct {
	Binary     string
	loopback   string
	rejectWith string
	is4        bool
}

var (
	// IPv4 is the IPv4 family.
	IPv4 = Family{Binary: "iptables", loopback: "127.0.0.0/8", rejectWith: "icmp-port-unreachable", is4: true}
	// IPv6 is the IPv6 family.
	IPv6 = Family{Binary: "ip6tables", loopback: "::1/128", rejectWith: "icmp6-port-unreachable"}
)

// Families lists the IP families gapdrill locks down.
var Families = []Family{IPv4, IPv6}

func (f Family) matches(a netip.Addr) bool { return a.Unmap().Is4() == f.is4 }

// Command is one iptables or ip6tables invocation.
type Command []string

func (c Command) String() string { return strings.Join(c, " ") }

// Rules returns the commands that leave family f able to reach only loopback,
// the drill's Docker network and the cluster's pod and service networks. Run
// them in order on each node.
//
// Allowed traffic jumps back with RETURN rather than ACCEPT, so the kube-proxy
// and CNI rules further down OUTPUT and FORWARD still apply.
func Rules(f Family, n Network) []Command {
	var cmds []Command
	add := func(args ...string) {
		cmds = append(cmds, append(Command{f.Binary, "-w", "5"}, args...))
	}
	add("-N", Chain)
	add("-A", Chain, "-o", "lo", "-j", "RETURN")
	add("-A", Chain, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "RETURN")
	for _, gw := range n.Gateways {
		if f.matches(gw) {
			add("-A", Chain, "-d", netip.PrefixFrom(gw.Unmap(), gw.Unmap().BitLen()).String(), "-j", "REJECT", "--reject-with", f.rejectWith)
		}
	}
	add("-A", Chain, "-d", f.loopback, "-j", "RETURN")
	for _, p := range n.allowed(f) {
		add("-A", Chain, "-d", p.String(), "-j", "RETURN")
	}
	add("-A", Chain, "-p", "tcp", "-j", "REJECT", "--reject-with", "tcp-reset")
	add("-A", Chain, "-j", "REJECT", "--reject-with", f.rejectWith)
	add("-I", "OUTPUT", "1", "-j", Chain)
	add("-I", "FORWARD", "1", "-j", Chain)
	return cmds
}

// Checks returns commands that succeed only if the chain is hooked into
// OUTPUT and FORWARD for family f.
func Checks(f Family) []Command {
	return []Command{
		{f.Binary, "-w", "5", "-C", "OUTPUT", "-j", Chain},
		{f.Binary, "-w", "5", "-C", "FORWARD", "-j", Chain},
	}
}

// CountCommand lists the chain with exact packet counters for family f.
func CountCommand(f Family) Command {
	return Command{f.Binary, "-w", "5", "-L", Chain, "-v", "-x", "-n"}
}

// RejectedPackets sums the packet counters of the REJECT rules in the output
// of CountCommand.
func RejectedPackets(out string) (int64, error) {
	var total int64
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		fields := strings.Fields(s.Text())
		// pkts bytes target prot opt in out source destination [extra]
		if len(fields) < 3 || fields[2] != "REJECT" {
			continue
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("unexpected counter %q in %q", fields[0], s.Text())
		}
		total += n
	}
	return total, s.Err()
}

func (n Network) allowed(f Family) []netip.Prefix {
	var out []netip.Prefix
	for _, p := range append(append([]netip.Prefix(nil), n.NodeSubnets...), n.PodCIDR, n.ServiceCIDR) {
		if p.IsValid() && f.matches(p.Addr()) {
			out = append(out, p.Masked())
		}
	}
	return out
}
