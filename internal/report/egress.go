// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"encoding/json"
	"sort"
	"strconv"
	"time"
)

// EgressBOM renders what the release tried to reach during the drill as a
// CycloneDX 1.6 BOM, so supply-chain tooling can read it: one service per
// outside host, marked as crossing a trust boundary, with the workloads that
// looked it up as properties. Endpoints use the "dns:" URI scheme (RFC 4501)
// because a lookup is what the drill observed. serial is the BOM's
// "urn:uuid:…" serial number.
func (r *Report) EgressBOM(serial string) ([]byte, error) {
	type lookup struct {
		workload string
		count    int
	}
	hosts := map[string]map[string]int{}
	add := func(f Finding) {
		if f.Kind != NetworkLookup || f.Host == "" {
			return
		}
		workload := "unknown workload"
		if f.Pod != nil && f.Pod.Owner != "" {
			workload = f.Pod.Owner
		} else if f.Pod != nil {
			workload = "pod " + f.Pod.Namespace + "/" + f.Pod.Name
		}
		if hosts[f.Host] == nil {
			hosts[f.Host] = map[string]int{}
		}
		hosts[f.Host][workload] += f.Count
	}
	for _, op := range r.Operations {
		for _, f := range op.Findings {
			add(f)
		}
	}
	for _, f := range r.Unattributed {
		add(f)
	}

	names := make([]string, 0, len(hosts))
	for h := range hosts {
		names = append(names, h)
	}
	sort.Strings(names)
	services := make([]cdxService, 0, len(names))
	for _, host := range names {
		var lookups []lookup
		for w, n := range hosts[host] {
			lookups = append(lookups, lookup{w, n})
		}
		sort.Slice(lookups, func(i, j int) bool { return lookups[i].workload < lookups[j].workload })
		svc := cdxService{
			BOMRef:        "egress:" + host,
			Name:          host,
			Endpoints:     []string{"dns:" + host},
			TrustBoundary: true,
		}
		for _, l := range lookups {
			svc.Properties = append(svc.Properties,
				cdxProperty{Name: "gapdrill:workload", Value: l.workload},
				cdxProperty{Name: "gapdrill:lookups", Value: strconv.Itoa(l.count)})
		}
		services = append(services, svc)
	}

	bom := cdxBOM{
		BOMFormat:    "CycloneDX",
		SpecVersion:  "1.6",
		SerialNumber: serial,
		Version:      1,
		Metadata: cdxMetadata{
			Timestamp: r.FinishedAt.UTC().Format(time.RFC3339),
			Tools:     cdxTools{Components: []cdxComponent{{Type: "application", Name: "gapdrill", Version: r.GapdrillVersion}}},
			Component: cdxComponent{Type: "application", Name: r.Drill},
		},
		Services: services,
	}
	b, err := json.MarshalIndent(bom, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

type cdxBOM struct {
	BOMFormat    string       `json:"bomFormat"`
	SpecVersion  string       `json:"specVersion"`
	SerialNumber string       `json:"serialNumber"`
	Version      int          `json:"version"`
	Metadata     cdxMetadata  `json:"metadata"`
	Services     []cdxService `json:"services"`
}

type cdxMetadata struct {
	Timestamp string       `json:"timestamp"`
	Tools     cdxTools     `json:"tools"`
	Component cdxComponent `json:"component"`
}

type cdxTools struct {
	Components []cdxComponent `json:"components"`
}

type cdxComponent struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type cdxService struct {
	BOMRef        string        `json:"bom-ref"`
	Name          string        `json:"name"`
	Endpoints     []string      `json:"endpoints"`
	TrustBoundary bool          `json:"x-trust-boundary"`
	Properties    []cdxProperty `json:"properties,omitempty"`
}

type cdxProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
