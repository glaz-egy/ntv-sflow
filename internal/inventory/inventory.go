// Package inventory loads what the operator knows about the network:
// devices (D-009 identities), networks, exporters (observation points),
// known topology links and static GeoIP overrides. Everything is optional:
// an empty inventory still works (endpoints stay unresolved, D-027).
package inventory

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"network-traffic-visualizer/internal/devices"
	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/topology"
)

type File struct {
	Exporters         []Exporter `yaml:"exporters"`
	ObservationPolicy *struct {
		External []string `yaml:"external"`
		Internal []string `yaml:"internal"`
	} `yaml:"observation_policy"`
	Networks     []Network     `yaml:"networks"`
	Devices      []Device      `yaml:"devices"`
	Topology     []Link        `yaml:"topology"`
	GeoOverrides []GeoOverride `yaml:"geo_overrides"`
}

type Exporter struct {
	ID              string  `yaml:"id"`
	Name            string  `yaml:"name"`
	AgentAddress    string  `yaml:"agent_address"`
	SubAgentID      uint32  `yaml:"sub_agent_id"`
	Role            string  `yaml:"role"`
	BoundaryIfIndex *int    `yaml:"boundary_if_index"`
	IfSpeedBps      float64 `yaml:"if_speed_bps"`
}

type Network struct {
	Name   string `yaml:"name"`
	CIDR   string `yaml:"cidr"`
	VlanID *int   `yaml:"vlan_id"`
}

// Address accepts either "10.0.0.1" or {address: 10.0.0.1, source: dhcp}.
type Address struct {
	Address string `yaml:"address"`
	Source  string `yaml:"source"`
}

func (a *Address) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		a.Address = n.Value
		return nil
	}
	type plain Address
	return n.Decode((*plain)(a))
}

type Device struct {
	ID        string    `yaml:"id"`
	Name      string    `yaml:"name"`
	Type      string    `yaml:"type"`
	Online    *bool     `yaml:"online"`
	Addresses []Address `yaml:"addresses"`
	Macs      []string  `yaml:"macs"`
	VlanID    *int      `yaml:"vlan_id"`
	SSID      *string   `yaml:"ssid"`
	Vendor    *string   `yaml:"vendor"`
	Model     *string   `yaml:"model"`
}

type Link struct {
	A          string  `yaml:"a"`
	B          string  `yaml:"b"`
	LinkType   string  `yaml:"link_type"`
	Evidence   string  `yaml:"evidence"`
	Confidence float64 `yaml:"confidence"`
	AInterface *string `yaml:"a_interface"`
	BInterface *string `yaml:"b_interface"`
}

type GeoOverride struct {
	Prefix       string   `yaml:"prefix"`
	CountryCode  *string  `yaml:"country_code"`
	CountryName  *string  `yaml:"country_name"`
	City         *string  `yaml:"city"`
	Latitude     *float64 `yaml:"latitude"`
	Longitude    *float64 `yaml:"longitude"`
	ASN          *int     `yaml:"asn"`
	Organization *string  `yaml:"organization"`
}

// Inventory is the validated, domain-typed result.
type Inventory struct {
	Exporters    []flow.Exporter
	Policy       flow.ObservationPolicy
	Networks     []topology.Network
	Devices      []devices.Device
	Links        []topology.Link
	GeoOverrides []enrichment.GeoOverride
	// AgentToExporter maps "<agent>/<sub-agent>" (collector identity) to exporter id.
	AgentToExporter map[string]string
}

var DefaultPolicy = flow.ObservationPolicy{
	External: []flow.ExporterRole{flow.RoleBoundary, flow.RoleCore, flow.RoleAccess},
	Internal: []flow.ExporterRole{flow.RoleCore, flow.RoleAccess, flow.RoleBoundary},
}

var (
	deviceTypes = set("router", "firewall", "switch", "wireless_ap", "server", "pc", "smartphone", "iot", "vm", "kubernetes_node", "unknown")
	addrSources = set("static", "dhcp", "arp", "nd", "dns", "wlc", "flow", "manual")
	linkTypes   = set("physical", "wireless_association", "logical", "unknown")
	evidences   = set("manual", "lldp", "cdp", "wlc", "inferred")
	roles       = set("boundary", "core", "access")
	idPattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
	macPattern  = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`)
)

func set(v ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range v {
		m[s] = true
	}
	return m
}

// Empty is the inventory used when no file is configured.
func Empty() *Inventory {
	return &Inventory{Policy: DefaultPolicy, AgentToExporter: map[string]string{}}
}

func Load(path string) (*Inventory, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read inventory: %w", err)
	}
	return Parse(raw)
}

// Parse decodes and validates an inventory document, reporting all problems.
func Parse(raw []byte) (*Inventory, error) {
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) { // empty file = empty inventory
		return nil, fmt.Errorf("parse inventory: %w", err)
	}
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	inv := Empty()

	ids := map[string]bool{topology.InternetNodeID: true}
	checkID := func(kind, id string) {
		switch {
		case isIP(id):
			add("%s id %q: an IP address is not a permanent identity (D-009)", kind, id)
		case !idPattern.MatchString(id):
			add("%s id %q: must start with a letter and use [A-Za-z0-9_.-] (max 64)", kind, id)
		case ids[id]:
			add("%s id %q: duplicate or reserved", kind, id)
		}
		ids[id] = true
	}

	for _, e := range f.Exporters {
		checkID("exporter", e.ID)
		agent, err := netip.ParseAddr(e.AgentAddress)
		if err != nil {
			add("exporter %q: agent_address %q is not an IP address", e.ID, e.AgentAddress)
			continue
		}
		if !roles[e.Role] {
			add("exporter %q: role must be boundary, core or access", e.ID)
		}
		name := e.Name
		if name == "" {
			name = e.ID
		}
		speed := e.IfSpeedBps
		key := agent.String() + "/" + fmt.Sprint(e.SubAgentID)
		if _, dup := inv.AgentToExporter[key]; dup {
			add("exporter %q: agent %s is configured twice", e.ID, key)
		}
		inv.AgentToExporter[key] = e.ID
		inv.Exporters = append(inv.Exporters, flow.Exporter{
			ID: e.ID, Name: name, AgentAddress: agent.String(), Role: flow.ExporterRole(e.Role),
			BoundaryIfIndex: e.BoundaryIfIndex, IfSpeedBps: speed,
		})
	}

	if p := f.ObservationPolicy; p != nil {
		conv := func(list []string, which string) []flow.ExporterRole {
			var out []flow.ExporterRole
			for _, r := range list {
				if !roles[r] {
					add("observation_policy.%s: unknown role %q", which, r)
				}
				out = append(out, flow.ExporterRole(r))
			}
			return out
		}
		inv.Policy = flow.ObservationPolicy{External: conv(p.External, "external"), Internal: conv(p.Internal, "internal")}
	}

	for _, n := range f.Networks {
		if _, err := netip.ParsePrefix(n.CIDR); err != nil {
			add("network %q: invalid cidr %q", n.Name, n.CIDR)
		}
		if n.VlanID != nil && (*n.VlanID < 0 || *n.VlanID > 4095) {
			add("network %q: vlan_id out of range", n.Name)
		}
		inv.Networks = append(inv.Networks, topology.Network{Name: n.Name, CIDR: n.CIDR, VlanID: n.VlanID})
	}

	seenAddr := map[string]string{}
	for _, d := range f.Devices {
		checkID("device", d.ID)
		if strings.HasPrefix(d.ID, "ep") && strings.Contains(d.ID, ":") {
			add("device %q: ids may not look like temporary endpoint ids", d.ID)
		}
		if !deviceTypes[d.Type] {
			add("device %q: unknown type %q", d.ID, d.Type)
		}
		dev := devices.Device{ID: d.ID, DisplayName: d.Name, Type: d.Type, Online: true, Macs: []string{},
			VlanID: d.VlanID, SSID: d.SSID, Vendor: d.Vendor, Model: d.Model}
		if dev.DisplayName == "" {
			dev.DisplayName = d.ID
		}
		if d.Online != nil {
			dev.Online = *d.Online
		}
		for _, a := range d.Addresses {
			ip, err := netip.ParseAddr(a.Address)
			if err != nil {
				add("device %q: invalid address %q", d.ID, a.Address)
				continue
			}
			src := a.Source
			if src == "" {
				src = "static"
			}
			if !addrSources[src] {
				add("device %q: unknown address source %q", d.ID, src)
			}
			if other, dup := seenAddr[ip.String()]; dup {
				// Never silently merge devices on weak evidence (DATA_MODEL §10).
				add("address %s is assigned to both %q and %q", ip, other, d.ID)
			}
			seenAddr[ip.String()] = d.ID
			fam := "ipv4"
			if ip.Is6() && !ip.Is4In6() {
				fam = "ipv6"
			}
			dev.Addresses = append(dev.Addresses, devices.Address{Address: ip.String(), Family: fam, Source: src})
		}
		for _, m := range d.Macs {
			if !macPattern.MatchString(m) {
				add("device %q: invalid MAC %q", d.ID, m)
			}
			dev.Macs = append(dev.Macs, strings.ToLower(m))
		}
		inv.Devices = append(inv.Devices, dev)
	}

	for i, l := range f.Topology {
		where := fmt.Sprintf("topology[%d] %s-%s", i, l.A, l.B)
		if !ids[l.A] || !ids[l.B] {
			add("%s: endpoints must be device ids or %q", where, topology.InternetNodeID)
		}
		if !linkTypes[l.LinkType] {
			add("%s: unknown link_type %q", where, l.LinkType)
		}
		if !evidences[l.Evidence] {
			add("%s: unknown evidence %q", where, l.Evidence)
		}
		if l.Confidence < 0 || l.Confidence > 1 {
			add("%s: confidence must be in [0, 1]", where)
		}
		inv.Links = append(inv.Links, topology.Link{
			ID: "link_" + l.A + "_" + l.B, A: l.A, B: l.B, LinkType: l.LinkType, Evidence: l.Evidence,
			Confidence: l.Confidence, AInterface: l.AInterface, BInterface: l.BInterface,
		})
	}

	for _, g := range f.GeoOverrides {
		p, err := netip.ParsePrefix(g.Prefix)
		if err != nil {
			if a, aerr := netip.ParseAddr(g.Prefix); aerr == nil {
				p, err = netip.PrefixFrom(a, a.BitLen()), nil
			}
		}
		if err != nil {
			add("geo_overrides: invalid prefix %q", g.Prefix)
			continue
		}
		if (g.Latitude == nil) != (g.Longitude == nil) {
			add("geo_overrides %s: latitude and longitude must be set together", g.Prefix)
		}
		if g.Latitude != nil && g.Longitude != nil && (*g.Latitude < -90 || *g.Latitude > 90 || *g.Longitude < -180 || *g.Longitude > 180) {
			add("geo_overrides %s: coordinates out of range", g.Prefix)
		}
		inv.GeoOverrides = append(inv.GeoOverrides, enrichment.GeoOverride{Prefix: p.Masked(), Record: enrichment.GeoRecord{
			CountryCode: g.CountryCode, CountryName: g.CountryName, City: g.City,
			Latitude: g.Latitude, Longitude: g.Longitude, ASN: g.ASN, Organization: g.Organization,
		}})
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return inv, nil
}

func isIP(s string) bool {
	_, err := netip.ParseAddr(s)
	return err == nil
}
