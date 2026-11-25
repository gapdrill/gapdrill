// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"testing"

	"github.com/gapdrill/gapdrill/internal/evidence"
)

func TestEgressBOM(t *testing.T) {
	in := demoInput()
	// A second workload looking up the same host, and one nobody can name.
	agent := &evidence.Pod{Namespace: "demo", Name: "agent-1", UID: "u-agent", Owner: "Deployment demo-agent"}
	in.Evidence = append(in.Evidence,
		evidence.Item{Kind: evidence.DNSQuery, Time: at(50), Pod: agent, Client: netip.MustParseAddr("10.244.0.12"), QName: "pypi.org", QType: "A"},
		evidence.Item{Kind: evidence.DNSQuery, Time: at(300), Client: netip.MustParseAddr("10.244.0.40"), QName: "telemetry.example.com", QType: "A"},
	)
	const serial = "urn:uuid:0b6a2b3c-1d4e-4f50-8a6b-7c8d9e0f1a2b"
	b, err := Build(in).EgressBOM(serial)
	if err != nil {
		t.Fatal(err)
	}

	var bom cdxBOM
	if err := json.Unmarshal(b, &bom); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, b)
	}
	if bom.BOMFormat != "CycloneDX" || bom.SpecVersion != "1.6" || bom.SerialNumber != serial || bom.Version != 1 {
		t.Errorf("header = %+v", bom)
	}
	if bom.Metadata.Component.Name != "demo" || bom.Metadata.Tools.Components[0].Name != "gapdrill" {
		t.Errorf("metadata = %+v", bom.Metadata)
	}

	want := []cdxService{
		{
			BOMRef: "egress:pypi.org", Name: "pypi.org", Endpoints: []string{"dns:pypi.org"}, TrustBoundary: true,
			Properties: []cdxProperty{
				{"gapdrill:workload", "Deployment demo-agent"}, {"gapdrill:lookups", "1"},
				{"gapdrill:workload", "Job demo-post-install (Helm post-install hook)"}, {"gapdrill:lookups", "6"},
			},
		},
		{
			BOMRef: "egress:telemetry.example.com", Name: "telemetry.example.com", Endpoints: []string{"dns:telemetry.example.com"}, TrustBoundary: true,
			Properties: []cdxProperty{{"gapdrill:workload", "unknown workload"}, {"gapdrill:lookups", "1"}},
		},
	}
	if !reflect.DeepEqual(bom.Services, want) {
		t.Errorf("services =\n%+v\nwant\n%+v", bom.Services, want)
	}
}

func TestEgressBOMWithNothingContacted(t *testing.T) {
	in := demoInput()
	in.Evidence = nil
	b, err := Build(in).EgressBOM("urn:uuid:00000000-0000-4000-8000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	var bom map[string]any
	if err := json.Unmarshal(b, &bom); err != nil {
		t.Fatal(err)
	}
	// An empty list, not a missing field: "contacted nothing" is a result.
	if s, ok := bom["services"].([]any); !ok || len(s) != 0 {
		t.Errorf("services = %#v, want []", bom["services"])
	}
}
