package mock

import (
	"network-traffic-visualizer/internal/devices"
	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/topology"
)

// Mock inventory (docs/MOCK_DATA.md §2–3). Mirrors inventory.ts.

func sp(s string) *string { return &s }
func ip(n int) *int       { return &n }

func v4(a string, src string) devices.Address {
	return devices.Address{Address: a, Family: "ipv4", Source: src}
}
func v6(a string, src string) devices.Address {
	return devices.Address{Address: a, Family: "ipv6", Source: src}
}

func device(id, name, typ string, addrs []devices.Address, opts func(*devices.Device)) devices.Device {
	d := devices.Device{ID: id, DisplayName: name, Type: typ, Online: true, Addresses: addrs, Macs: []string{}}
	if opts != nil {
		opts(&d)
	}
	return d
}

func Networks() []topology.Network {
	return []topology.Network{
		{Name: "mgmt", CIDR: "10.0.0.0/24", VlanID: ip(1)},
		{Name: "servers", CIDR: "10.10.0.0/24", VlanID: ip(10)},
		{Name: "clients", CIDR: "10.20.0.0/24", VlanID: ip(20)},
		{Name: "iot", CIDR: "10.30.0.0/24", VlanID: ip(30)},
		{Name: "wireless", CIDR: "10.40.0.0/24", VlanID: ip(40)},
		{Name: "servers-v6", CIDR: "fd00:10::/64", VlanID: ip(10)},
		{Name: "clients-v6", CIDR: "fd00:20::/64", VlanID: ip(20)},
		{Name: "wireless-v6", CIDR: "fd00:40::/64", VlanID: ip(40)},
	}
}

func Devices() []devices.Device {
	return []devices.Device{
		device("dev_router", "home-router", "router", []devices.Address{v4("10.0.0.1", "static")}, func(d *devices.Device) {
			d.VlanID, d.Vendor, d.Model, d.Macs = ip(1), sp("Mock Networks"), sp("MR-1000"), []string{"02:00:00:00:00:01"}
		}),
		device("dev_core_sw", "core-switch", "switch", []devices.Address{v4("10.0.0.2", "static")}, func(d *devices.Device) {
			d.VlanID, d.Vendor, d.Model, d.Macs = ip(1), sp("Mock Networks"), sp("MS-24"), []string{"02:00:00:00:00:02"}
		}),
		device("dev_access_sw", "access-switch-01", "switch", []devices.Address{v4("10.0.0.3", "static")}, func(d *devices.Device) {
			d.VlanID, d.Macs = ip(1), []string{"02:00:00:00:00:03"}
		}),
		device("dev_ap_main", "ap-main", "wireless_ap", []devices.Address{v4("10.0.0.4", "static")}, func(d *devices.Device) {
			d.VlanID, d.Macs = ip(1), []string{"02:00:00:00:00:04"}
		}),
		device("dev_nas01", "nas-01", "server", []devices.Address{v4("10.10.0.10", "static"), v6("fd00:10::10", "static")}, func(d *devices.Device) {
			d.VlanID, d.Vendor, d.Macs = ip(10), sp("Mock Storage"), []string{"02:00:00:00:10:10"}
		}),
		device("dev_k8s01", "k8s-node-01", "kubernetes_node", []devices.Address{v4("10.10.0.20", "static")}, func(d *devices.Device) {
			d.VlanID, d.Macs = ip(10), []string{"02:00:00:00:10:20"}
		}),
		device("dev_media", "media-server", "server", []devices.Address{v4("10.10.0.30", "static")}, func(d *devices.Device) {
			d.VlanID, d.Macs = ip(10), []string{"02:00:00:00:10:30"}
		}),
		device("dev_pc01", "pc-01", "pc", []devices.Address{v4("10.20.0.10", "dhcp"), v6("fd00:20::10", "nd")}, func(d *devices.Device) {
			d.VlanID, d.Macs = ip(20), []string{"02:00:00:00:20:10"}
		}),
		device("dev_laptop01", "laptop-01", "pc", []devices.Address{v4("10.40.0.21", "dhcp")}, func(d *devices.Device) {
			d.VlanID, d.SSID, d.Macs = ip(40), sp("home-5g"), []string{"02:00:00:00:40:21"}
		}),
		device("dev_phone01", "phone-01", "smartphone", []devices.Address{v4("10.40.0.22", "dhcp"), v6("fd00:40::22", "nd")}, func(d *devices.Device) {
			d.VlanID, d.SSID = ip(40), sp("home-5g")
		}),
		// No "tablet" device type exists; see docs/DECISIONS.md D-030.
		device("dev_tablet01", "tablet-01", "smartphone", []devices.Address{v4("10.40.0.23", "dhcp")}, func(d *devices.Device) {
			d.VlanID, d.SSID = ip(40), sp("home-5g")
		}),
		device("dev_thermostat", "iot-thermostat", "iot", []devices.Address{v4("10.30.0.40", "dhcp")}, func(d *devices.Device) {
			d.VlanID, d.SSID = ip(30), sp("home-iot")
		}),
		device("dev_camera", "iot-camera", "iot", []devices.Address{v4("10.30.0.41", "dhcp")}, func(d *devices.Device) {
			d.VlanID = ip(30)
		}),
	}
}

func Exporters() []flow.Exporter {
	return []flow.Exporter{
		{ID: "exp_router", Name: "home-router", AgentAddress: "10.0.0.1", Role: flow.RoleBoundary, SamplingRate: 256, BoundaryIfIndex: ip(1), IfSpeedBps: 10_000_000_000},
		{ID: "exp_core", Name: "core-switch", AgentAddress: "10.0.0.2", Role: flow.RoleCore, SamplingRate: 512, IfSpeedBps: 10_000_000_000},
	}
}

func link(a, b, evidence string, confidence float64, aIf, bIf *string, linkType string) topology.Link {
	return topology.Link{ID: "link_" + a + "_" + b, A: a, B: b, LinkType: linkType, Evidence: evidence, Confidence: confidence, AInterface: aIf, BInterface: bIf}
}

func Topology() []topology.Link {
	const phys, wifi = "physical", "wireless_association"
	return []topology.Link{
		link(topology.InternetNodeID, "dev_router", "manual", 1, nil, sp("wan0"), "logical"),
		link("dev_router", "dev_core_sw", "manual", 1, sp("lan0"), sp("Gi1/0/1"), phys),
		link("dev_core_sw", "dev_nas01", "lldp", 0.95, sp("Gi1/0/3"), sp("eth0"), phys),
		link("dev_core_sw", "dev_k8s01", "lldp", 0.95, sp("Gi1/0/4"), sp("eno1"), phys),
		// Inferred from the MAC table only — must render as uncertain.
		link("dev_core_sw", "dev_media", "inferred", 0.5, sp("Gi1/0/5"), nil, phys),
		link("dev_core_sw", "dev_access_sw", "lldp", 0.95, sp("Gi1/0/6"), sp("Gi0/1"), phys),
		link("dev_core_sw", "dev_ap_main", "lldp", 0.95, sp("Gi1/0/7"), sp("eth0"), phys),
		link("dev_access_sw", "dev_pc01", "manual", 1, sp("Gi0/2"), nil, phys),
		link("dev_access_sw", "dev_camera", "inferred", 0.6, sp("Gi0/5"), nil, phys),
		link("dev_ap_main", "dev_laptop01", "wlc", 0.9, sp("radio1"), nil, wifi),
		link("dev_ap_main", "dev_phone01", "wlc", 0.9, sp("radio1"), nil, wifi),
		link("dev_ap_main", "dev_tablet01", "wlc", 0.9, sp("radio1"), nil, wifi),
		link("dev_ap_main", "dev_thermostat", "wlc", 0.9, sp("radio0"), nil, wifi),
	}
}

// corePorts is the core-switch ifIndex per directly attached device.
var corePorts = map[string]int{
	"dev_router": 1, "dev_nas01": 3, "dev_k8s01": 4, "dev_media": 5, "dev_access_sw": 6, "dev_ap_main": 7,
}

// DefaultInternalCIDRs equals configs/config.example.yaml network.internal_cidrs.
var DefaultInternalCIDRs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}

var DefaultOrigin = topology.Origin{Label: "Home Network", Latitude: 35.68, Longitude: 139.76, Precision: "city"}

var DefaultPolicy = flow.ObservationPolicy{
	External: []flow.ExporterRole{flow.RoleBoundary, flow.RoleCore, flow.RoleAccess},
	Internal: []flow.ExporterRole{flow.RoleCore, flow.RoleAccess, flow.RoleBoundary},
}

// Inventory is the scenario-adjustable metadata (what the system *knows*).
type Inventory struct {
	Devices       []devices.Device
	Networks      []topology.Network
	Exporters     []flow.Exporter
	Topology      []topology.Link
	InternalCIDRs []string
	Origin        topology.Origin
	Policy        flow.ObservationPolicy
}

func DefaultInventory() Inventory {
	return Inventory{
		Devices: Devices(), Networks: Networks(), Exporters: Exporters(), Topology: Topology(),
		InternalCIDRs: append([]string(nil), DefaultInternalCIDRs...),
		Origin:        DefaultOrigin, Policy: DefaultPolicy,
	}
}
