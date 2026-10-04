package aggregation

import (
	"testing"

	"network-traffic-visualizer/internal/devices"
	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/flow"
)

func p[T any](v T) *T { return &v }

func testContext(t *testing.T) Context {
	c, err := enrichment.NewClassifier([]string{"10.0.0.0/8", "fc00::/7"})
	if err != nil {
		t.Fatal(err)
	}
	return Context{
		Classifier: c,
		Registry: devices.NewRegistry([]devices.Device{{
			ID: "dev_pc01", Addresses: []devices.Address{{Address: "10.20.0.10"}, {Address: "fd00:20::10"}},
		}}),
		Geo: enrichment.MapGeo{},
		Exporters: []flow.Exporter{
			{ID: "exp_router", Role: flow.RoleBoundary},
			{ID: "exp_core", Role: flow.RoleCore},
		},
		Policy: flow.ObservationPolicy{
			External: []flow.ExporterRole{flow.RoleBoundary, flow.RoleCore, flow.RoleAccess},
			Internal: []flow.ExporterRole{flow.RoleCore, flow.RoleAccess, flow.RoleBoundary},
		},
	}
}

func obs(exporter, src, dst string, samples int, bytes float64) flow.WindowObservation {
	return flow.WindowObservation{
		ExporterID: exporter, SrcIP: src, DstIP: dst, Protocol: flow.TCP,
		SrcPort: p(50000), DstPort: p(443), SamplingRate: 512, SampleCount: samples,
		EstimatedBytes: bytes, LastSampleAt: p(5),
	}
}

func TestDoesNotSumObservationPoints(t *testing.T) {
	flows := Attribute([]flow.WindowObservation{
		obs("exp_core", "10.20.0.10", "198.51.100.10", 10, 5_120_000),
		obs("exp_router", "10.20.0.10", "198.51.100.10", 21, 5_376_000),
	}, testContext(t))
	if len(flows) != 1 {
		t.Fatalf("want 1 flow, got %d", len(flows))
	}
	f := flows[0]
	if f.Used.ExporterID != "exp_router" || f.EstimatedBytes != 5_376_000 || len(f.Observations) != 2 {
		t.Fatalf("external flow should use boundary exporter only: %+v", f)
	}
	if f.Direction != Outbound || *f.InternalNodeID != "dev_pc01" {
		t.Fatalf("unexpected attribution: %+v", f)
	}
}

func TestInternalPrefersCore(t *testing.T) {
	flows := Attribute([]flow.WindowObservation{
		obs("exp_router", "10.20.0.10", "10.10.0.10", 1, 1),
		obs("exp_core", "10.20.0.10", "10.10.0.10", 2, 2),
	}, testContext(t))
	if flows[0].Used.ExporterID != "exp_core" || flows[0].Scope != ScopeInternal {
		t.Fatalf("got %+v", flows[0])
	}
}

func TestIdentityAndUnknowns(t *testing.T) {
	ctx := testContext(t)
	flows := Attribute([]flow.WindowObservation{
		obs("exp_core", "fd00:20::10", "2001:db8:1::10", 1, 1),
		obs("exp_core", "10.30.0.77", "192.0.2.250", 1, 1),
		obs("exp_core", "bogus", "192.0.2.250", 1, 1),
	}, ctx)
	if len(flows) != 2 {
		t.Fatalf("unparseable addresses must be dropped, got %d flows", len(flows))
	}
	bySrc := map[string]AttributedFlow{}
	for _, f := range flows {
		bySrc[f.SrcIP] = f
	}
	if *bySrc["fd00:20::10"].InternalNodeID != "dev_pc01" {
		t.Errorf("IPv6 address should resolve to the same device")
	}
	unresolved := bySrc["10.30.0.77"]
	if *unresolved.InternalNodeID != "ep:10.30.0.77" {
		t.Errorf("unresolved endpoint should get a temporary id")
	}
	if unresolved.Geo.Latitude != nil || unresolved.Geo.ASN != nil {
		t.Errorf("unknown geo must stay unknown")
	}
}

func TestOrderIndependence(t *testing.T) {
	ctx := testContext(t)
	a := []flow.WindowObservation{
		obs("exp_core", "10.20.0.10", "198.51.100.10", 1, 1),
		obs("exp_router", "10.20.0.10", "198.51.100.10", 2, 2),
		obs("exp_core", "10.40.0.21", "203.0.113.40", 3, 3),
	}
	b := []flow.WindowObservation{a[2], a[1], a[0]}
	fa, fb := Attribute(a, ctx), Attribute(b, ctx)
	for i := range fa {
		if fa[i].Key != fb[i].Key || fa[i].Used.ExporterID != fb[i].Used.ExporterID || len(fa[i].Observations) != len(fb[i].Observations) {
			t.Fatalf("arrival order changed the result at %d", i)
		}
	}
}

func TestDestinationKeys(t *testing.T) {
	g := enrichment.GeoRecord{CountryCode: p("JP"), City: p("Tokyo"), ASN: p(13335)}
	if k := DestinationKey("city", "198.51.100.10", g); k != "city:JP:Tokyo" {
		t.Error(k)
	}
	gr, v, ok := ParseDestinationKey("ip:2001:db8::1")
	if !ok || gr != "ip" || v != "2001:db8::1" {
		t.Error("IPv6 key should split on the first colon only")
	}
	if _, _, ok := ParseDestinationKey("planet:earth"); ok {
		t.Error("unknown grouping should be rejected")
	}
	if DestinationKey("asn", "x", enrichment.GeoRecord{}) != "asn:unknown" {
		t.Error("unknown ASN key")
	}
}
