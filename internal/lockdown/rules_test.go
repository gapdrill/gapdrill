// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package lockdown

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

var testNet = Network{
	NodeSubnets: []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16"), netip.MustParsePrefix("fc00:f853:ccd:e793::/64")},
	Gateways:    []netip.Addr{netip.MustParseAddr("172.18.0.1"), netip.MustParseAddr("fc00:f853:ccd:e793::1")},
	PodCIDR:     netip.MustParsePrefix("10.244.0.0/16"),
	ServiceCIDR: netip.MustParsePrefix("10.96.0.0/16"),
}

func TestRulesIPv4(t *testing.T) {
	got := join(Rules(IPv4, testNet))
	want := `iptables -w 5 -N GAPDRILL-EGRESS
iptables -w 5 -A GAPDRILL-EGRESS -o lo -j RETURN
iptables -w 5 -A GAPDRILL-EGRESS -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
iptables -w 5 -A GAPDRILL-EGRESS -d 172.18.0.1/32 -j REJECT --reject-with icmp-port-unreachable
iptables -w 5 -A GAPDRILL-EGRESS -d 127.0.0.0/8 -j RETURN
iptables -w 5 -A GAPDRILL-EGRESS -d 172.18.0.0/16 -j RETURN
iptables -w 5 -A GAPDRILL-EGRESS -d 10.244.0.0/16 -j RETURN
iptables -w 5 -A GAPDRILL-EGRESS -d 10.96.0.0/16 -j RETURN
iptables -w 5 -A GAPDRILL-EGRESS -p tcp -j REJECT --reject-with tcp-reset
iptables -w 5 -A GAPDRILL-EGRESS -j REJECT --reject-with icmp-port-unreachable
iptables -w 5 -I OUTPUT 1 -j GAPDRILL-EGRESS
iptables -w 5 -I FORWARD 1 -j GAPDRILL-EGRESS`
	if got != want {
		t.Errorf("IPv4 rules:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestRulesIPv6OnlyAllowsIPv6Networks(t *testing.T) {
	got := join(Rules(IPv6, testNet))
	for _, want := range []string{
		"ip6tables -w 5 -A GAPDRILL-EGRESS -d fc00:f853:ccd:e793::1/128 -j REJECT --reject-with icmp6-port-unreachable",
		"ip6tables -w 5 -A GAPDRILL-EGRESS -d ::1/128 -j RETURN",
		"ip6tables -w 5 -A GAPDRILL-EGRESS -d fc00:f853:ccd:e793::/64 -j RETURN",
		"ip6tables -w 5 -A GAPDRILL-EGRESS -j REJECT --reject-with icmp6-port-unreachable",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "10.244.0.0") || strings.Contains(got, "172.18.0") {
		t.Errorf("IPv4 networks leaked into ip6tables rules:\n%s", got)
	}
}

func TestNodesOnlineCutsOffOnlyThePods(t *testing.T) {
	n := testNet
	n.NodesOnline = true
	rules := join(Rules(IPv4, n))
	if strings.Contains(rules, "-I OUTPUT") {
		t.Errorf("with the nodes online, the chain must not be hooked into OUTPUT:\n%s", rules)
	}
	if !strings.Contains(rules, "-I FORWARD 1 -j "+Chain) {
		t.Errorf("the pods must still be cut off:\n%s", rules)
	}
	checks := join(Checks(IPv4, n))
	if strings.Contains(checks, "OUTPUT") || !strings.Contains(checks, "-C FORWARD -j "+Chain) {
		t.Errorf("checks must match the hooks:\n%s", checks)
	}
	if all := join(Checks(IPv4, testNet)); !strings.Contains(all, "-C OUTPUT") || !strings.Contains(all, "-C FORWARD") {
		t.Errorf("by default both hooks are checked:\n%s", all)
	}
}

func TestGatewayRejectComesBeforeNodeSubnetReturn(t *testing.T) {
	rules := join(Rules(IPv4, testNet))
	if strings.Index(rules, "-d 172.18.0.1/32 -j REJECT") > strings.Index(rules, "-d 172.18.0.0/16 -j RETURN") {
		t.Fatal("the gateway (host) must be rejected before the node subnet is allowed")
	}
}

func TestRejectedPackets(t *testing.T) {
	out := `Chain GAPDRILL-EGRESS (2 references)
    pkts      bytes target     prot opt in     out     source               destination
      12      720 RETURN     all  --  *      lo      0.0.0.0/0            0.0.0.0/0
    3051  1200000 RETURN     all  --  *      *       0.0.0.0/0            0.0.0.0/0            ctstate RELATED,ESTABLISHED
       2      120 REJECT     all  --  *      *       0.0.0.0/0            172.18.0.1           reject-with icmp-port-unreachable
       7      420 REJECT     tcp  --  *      *       0.0.0.0/0            0.0.0.0/0            reject-with tcp-reset
       5      300 REJECT     all  --  *      *       0.0.0.0/0            0.0.0.0/0            reject-with icmp-port-unreachable
`
	n, err := RejectedPackets(out)
	if err != nil || n != 14 {
		t.Fatalf("RejectedPackets = %d, %v; want 14", n, err)
	}
}

type fakeNode struct {
	ran  []string
	fail func(cmd string) bool
	out  map[string]string
}

func (f *fakeNode) Exec(_ context.Context, _ string, args ...string) (string, error) {
	cmd := strings.Join(args, " ")
	f.ran = append(f.ran, cmd)
	if f.fail != nil && f.fail(cmd) {
		return "", errors.New("exit status 1")
	}
	return f.out[cmd], nil
}

func TestApplySkipsIPv6OnlyWhenNodeHasNoIPv6(t *testing.T) {
	noIP6 := &fakeNode{fail: func(c string) bool { return strings.HasPrefix(c, "ip6tables") }}
	warnings, err := Apply(context.Background(), noIP6, "n1", testNet)
	if err != nil || len(warnings) != 1 {
		t.Fatalf("want one warning and no error, got %v, %v", warnings, err)
	}

	withIP6 := &fakeNode{
		fail: func(c string) bool { return strings.HasPrefix(c, "ip6tables") },
		out:  map[string]string{"ip -6 addr show scope global": "inet6 fc00::2/64 scope global"},
	}
	if _, err := Apply(context.Background(), withIP6, "n1", testNet); err == nil {
		t.Fatal("a node with IPv6 must not be left unlocked")
	}

	noIP4 := &fakeNode{fail: func(c string) bool { return strings.HasPrefix(c, "iptables") }}
	if _, err := Apply(context.Background(), noIP4, "n1", testNet); err == nil {
		t.Fatal("IPv4 failures are always fatal")
	}
}

func join(cmds []Command) string {
	lines := make([]string, len(cmds))
	for i, c := range cmds {
		lines[i] = c.String()
	}
	return strings.Join(lines, "\n")
}
