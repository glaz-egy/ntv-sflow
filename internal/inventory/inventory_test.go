package inventory

import (
	"reflect"
	"strings"
	"testing"

	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/mock"
)

// The demo inventory must describe exactly the mock's network, so that live
// mode fed by sflow-gen reproduces mock mode.
func TestDemoInventoryMatchesMock(t *testing.T) {
	inv, err := Load("../../configs/inventory.demo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := mock.DefaultInventory()
	// In live mode the sampling rate arrives with every sample; it is not inventory.
	for i := range m.Exporters {
		m.Exporters[i].SamplingRate = 0
	}
	if !reflect.DeepEqual(inv.Devices, m.Devices) {
		t.Errorf("devices differ:\n%+v\n%+v", inv.Devices, m.Devices)
	}
	if !reflect.DeepEqual(inv.Exporters, m.Exporters) {
		t.Errorf("exporters differ:\n%+v\n%+v", inv.Exporters, m.Exporters)
	}
	if !reflect.DeepEqual(inv.Links, m.Topology) {
		t.Errorf("topology differs")
	}
	if !reflect.DeepEqual(inv.Networks, m.Networks) || !reflect.DeepEqual(inv.Policy, m.Policy) {
		t.Errorf("networks/policy differ")
	}
	geo := enrichment.NewChainGeo(enrichment.NewOverrideGeo(inv.GeoOverrides), nil, 100)
	for ip, want := range mock.BaseGeoDB() {
		if got := geo.Lookup(ip); !reflect.DeepEqual(got, want) {
			t.Errorf("geo %s: got %+v want %+v", ip, got, want)
		}
	}
	if inv.AgentToExporter["10.0.0.1/0"] != "exp_router" {
		t.Errorf("agent mapping: %v", inv.AgentToExporter)
	}
}

func TestExampleInventoryIsValid(t *testing.T) {
	if _, err := Load("../../configs/inventory.example.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyInventory(t *testing.T) {
	inv, err := Parse(nil)
	if err != nil || len(inv.Devices) != 0 || len(inv.Policy.External) != 3 {
		t.Fatalf("%+v %v", inv, err)
	}
}

func TestValidation(t *testing.T) {
	_, err := Parse([]byte(`
exporters:
  - { id: e1, agent_address: nope, role: core }
  - { id: e2, agent_address: 10.0.0.1, role: spine }
devices:
  - { id: 10.0.0.5, type: pc }
  - { id: d1, type: toaster, addresses: [10.0.0.9, bogus], macs: [zz] }
  - { id: d2, type: pc, addresses: [10.0.0.9] }
  - { id: d2, type: pc }
topology:
  - { a: d1, b: ghost, link_type: physical, evidence: guessed, confidence: 2 }
geo_overrides:
  - { prefix: 198.51.100.0/24, latitude: 10 }
`))
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{
		"agent_address", "role must be", "not a permanent identity", "unknown type", "invalid address",
		"invalid MAC", "assigned to both", "duplicate", "endpoints must be", "unknown evidence",
		"confidence", "set together",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestUnknownKeysRejected(t *testing.T) {
	if _, err := Parse([]byte("devices:\n  - { id: d1, type: pc, adresses: [10.0.0.1] }\n")); err == nil {
		t.Fatal("typo should be an error")
	}
}
