// Package devices holds device identities (D-009): an IP address is never a
// permanent identity; a device has an opaque id and may have many addresses.
package devices

import "network-traffic-visualizer/internal/enrichment"

type Address struct {
	Address string
	Family  string // ipv4 | ipv6
	Source  string // static | dhcp | arp | nd | dns | wlc | flow | manual
}

type Device struct {
	ID          string
	DisplayName string
	Type        string
	Online      bool
	Addresses   []Address
	Macs        []string
	VlanID      *int
	SSID        *string
	Vendor      *string
	Model       *string
}

// UnresolvedNodeID is the temporary identity of an internal address with no
// known device (D-027).
func UnresolvedNodeID(ip string) string { return "ep:" + ip }

// Registry resolves addresses to devices.
type Registry struct {
	Devices []Device
	byIP    map[string]int
	byID    map[string]int
}

func NewRegistry(devs []Device) *Registry {
	r := &Registry{Devices: devs, byIP: map[string]int{}, byID: map[string]int{}}
	for i, d := range devs {
		r.byID[d.ID] = i
		for _, a := range d.Addresses {
			if k := enrichment.CanonicalIP(a.Address); k != "" {
				r.byIP[k] = i
			}
		}
	}
	return r
}

func (r *Registry) Lookup(ip string) *Device {
	if i, ok := r.byIP[enrichment.CanonicalIP(ip)]; ok {
		return &r.Devices[i]
	}
	return nil
}

func (r *Registry) Get(id string) *Device {
	if i, ok := r.byID[id]; ok {
		return &r.Devices[i]
	}
	return nil
}

// NodeIDFor returns the device id, or a temporary endpoint id.
func (r *Registry) NodeIDFor(ip string) string {
	if d := r.Lookup(ip); d != nil {
		return d.ID
	}
	return UnresolvedNodeID(ip)
}
