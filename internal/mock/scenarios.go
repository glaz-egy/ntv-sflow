package mock

import (
	"fmt"
	"strconv"

	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/flow"
)

// Mirrors scenarios.ts. Order of conversations matters (ports, plans).

const (
	mbps = 1_000_000
	// StartTick is the sim second mock providers start at (full first window).
	StartTick = 30
)

type RateProfile struct {
	Bps           float64
	Shape         string // "" | steady | burst | periodic
	PeriodSeconds int
	Duty          float64
	Jitter        float64
}

type Conversation struct {
	ID          string
	Client      string
	Server      string
	Protocol    flow.Protocol
	ServerPort  int
	ClientPort  int
	Up          RateProfile
	Down        RateProfile
	PacketBytes *[2]int // up, down
}

type Scenario struct {
	Name            string
	Description     string
	Conversations   []Conversation
	Geo             enrichment.MapGeo
	Inventory       Inventory
	DataStopsAtTick *int
}

var ScenarioNames = []string{"default", "heavy-download", "internal-backup", "many-destinations", "unknown-metadata", "stale-collector"}

func IsScenarioName(s string) bool {
	for _, n := range ScenarioNames {
		if n == s {
			return true
		}
	}
	return false
}

type convBuilder struct{ nextPort int }

func (b *convBuilder) conv(id, client, server string, proto flow.Protocol, port int, upMbps, downMbps float64) Conversation {
	c := Conversation{
		ID: id, Client: client, Server: server, Protocol: proto, ServerPort: port, ClientPort: b.nextPort,
		Up:   RateProfile{Bps: upMbps * mbps, Jitter: 0.25},
		Down: RateProfile{Bps: downMbps * mbps, Jitter: 0.25},
	}
	b.nextPort++
	return c
}

func (b *convBuilder) base() []Conversation {
	tcp, udp := flow.TCP, flow.UDP
	// Same order as the TypeScript: client ports are assigned sequentially.
	out := []Conversation{
		b.conv("pc-nas-smb", "10.20.0.10", "10.10.0.10", tcp, 445, 420, 18),
		b.conv("pc-nas-ssh6", "fd00:20::10", "fd00:10::10", tcp, 22, 22, 1.5),
		b.conv("pc-google", "10.20.0.10", "203.0.113.20", udp, 443, 3, 55),
		b.conv("pc-cloudflare", "10.20.0.10", "198.51.100.10", tcp, 443, 2, 22),
		b.conv("phone-cloudflare6", "fd00:40::22", "2001:db8:1::10", tcp, 443, 4, 30),
		b.conv("phone-google", "10.40.0.22", "203.0.113.21", udp, 443, 1.5, 12),
		b.conv("media-amazon", "10.10.0.30", "192.0.2.30", tcp, 443, 260, 3),
		b.conv("laptop-media", "10.40.0.21", "10.10.0.30", tcp, 32400, 1, 38),
		b.conv("thermostat-amazon", "10.30.0.40", "192.0.2.31", tcp, 8883, 0.25, 0.12),
		b.conv("camera-unknown-geo", "10.30.0.41", "198.51.100.200", tcp, 443, 3.5, 0.3),
		b.conv("k8s-nas-nfs", "10.10.0.20", "10.10.0.10", tcp, 2049, 70, 25),
		b.conv("k8s-microsoft", "10.10.0.20", "203.0.113.41", tcp, 443, 2, 14),
		b.conv("laptop-microsoft", "10.40.0.21", "203.0.113.40", tcp, 443, 4, 18),
		b.conv("laptop-cloudflare", "10.40.0.21", "198.51.100.11", tcp, 443, 1, 7),
		b.conv("tablet-akamai", "10.40.0.23", "198.51.100.50", tcp, 443, 1, 32),
		b.conv("tablet-netflix", "10.40.0.23", "203.0.113.80", tcp, 443, 0.8, 18),
		b.conv("unresolved-unknown", "10.30.0.77", "192.0.2.250", tcp, 8443, 1.2, 0.6),
	}
	// Media server → external burst (offsite backup).
	out[6].Up = RateProfile{Bps: 260 * mbps, Shape: "burst", PeriodSeconds: 60, Duty: 0.35, Jitter: 0.2}
	// IoT → small periodic external traffic.
	out[8].Up = RateProfile{Bps: 0.25 * mbps, Shape: "periodic", PeriodSeconds: 30, Duty: 0.3, Jitter: 0.3}
	out[8].Down = RateProfile{Bps: 0.12 * mbps, Shape: "periodic", PeriodSeconds: 30, Duty: 0.3, Jitter: 0.3}
	out[8].PacketBytes = &[2]int{180, 140}
	return out
}

func boost(cs []Conversation, id string, upMbps, downMbps float64) {
	for i := range cs {
		if cs[i].ID == id {
			cs[i].Up.Bps = upMbps * mbps
			cs[i].Down.Bps = downMbps * mbps
			return
		}
	}
	panic("unknown conversation " + id)
}

func (b *convBuilder) manyDestinations(seed int) ([]Conversation, enrichment.MapGeo) {
	r := KeyedRand(seed, "many-destinations")
	clients := []string{"10.20.0.10", "10.40.0.21", "10.40.0.22", "10.40.0.23", "10.10.0.20", "10.10.0.30"}
	geo := enrichment.MapGeo{}
	var convs []Conversation
	for i := 0; i < 140; i++ {
		var addr string
		if i < 100 {
			prefix := "203.0.113"
			if i%2 == 0 {
				prefix = "198.51.100"
			}
			addr = prefix + "." + strconv.Itoa(100+i/2)
		} else {
			addr = "2001:db8:ff::" + strconv.FormatInt(int64(i-99), 16)
		}
		city := Cities[int(r.Float()*float64(len(Cities)))]
		org := Orgs[int(r.Float()*float64(len(Orgs)))]
		geo[addr] = cityGeo(city, org.ASN, org.Organization)
		client := clients[int(r.Float()*float64(len(clients)))]
		clientIP := client
		if containsColon(addr) {
			clientIP = "fd00:20::10"
		}
		// 0.3–20 Mbps, skewed towards small flows (arithmetic only, D-039).
		u := r.Float()
		down := 0.3 + float64(19.7*u*u*u)
		proto := flow.UDP
		if r.Float() < 0.7 {
			proto = flow.TCP
		}
		convs = append(convs, b.conv(fmt.Sprintf("many-%d", i), clientIP, addr, proto, 443, down/8, down))
	}
	return convs, geo
}

func containsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}

// BuildScenario mirrors buildScenario() in scenarios.ts.
func BuildScenario(name string, seed int) (Scenario, error) {
	if !IsScenarioName(name) {
		return Scenario{}, fmt.Errorf("unknown mock scenario %q", name)
	}
	b := &convBuilder{nextPort: 49152}
	convs := b.base()
	geo := BaseGeoDB()
	inv := DefaultInventory()
	sc := Scenario{Name: name, Description: "Balanced household traffic."}

	switch name {
	case "heavy-download":
		sc.Description = "pc-01 downloads a large game update from a single CDN."
		convs = append(convs, b.conv("pc-valve", "10.20.0.10", "203.0.113.60", flow.TCP, 443, 6, 620))
	case "internal-backup":
		sc.Description = "Backups to nas-01 dominate the internal view."
		boost(convs, "pc-nas-smb", 880, 25)
		boost(convs, "k8s-nas-nfs", 300, 40)
		convs = append(convs, b.conv("media-nas-backup", "10.10.0.30", "10.10.0.10", flow.TCP, 873, 410, 6))
	case "many-destinations":
		sc.Description = "Many small destinations to exercise grouping and top-N."
		extra, extraGeo := b.manyDestinations(seed)
		convs = append(convs, extra...)
		for k, v := range extraGeo {
			geo[k] = v
		}
	case "unknown-metadata":
		sc.Description = "Missing geo, unresolved devices and partial topology."
		for _, a := range []string{"203.0.113.41", "198.51.100.11"} {
			if r, ok := geo[a]; ok {
				r.Latitude, r.Longitude, r.City = nil, nil, nil
				geo[a] = r
			}
		}
		delete(geo, "203.0.113.80")
		kept := inv.Devices[:0]
		for _, d := range inv.Devices {
			if d.ID != "dev_laptop01" && d.ID != "dev_tablet01" {
				kept = append(kept, d)
			}
		}
		inv.Devices = kept
		links := inv.Topology[:0]
		for _, l := range inv.Topology {
			if l.Evidence != "wlc" && l.B != "dev_media" {
				links = append(links, l)
			}
		}
		inv.Topology = links
	case "stale-collector":
		sc.Description = "Collector stops delivering data 20 s after start."
		sc.DataStopsAtTick = ip(StartTick + 20)
	}
	sc.Conversations, sc.Geo, sc.Inventory = convs, geo, inv
	return sc, nil
}
