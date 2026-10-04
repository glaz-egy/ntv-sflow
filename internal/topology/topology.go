// Package topology holds known physical/logical links. Every link carries
// its evidence and confidence; nothing here is inferred from flows alone.
package topology

// InternetNodeID is the external aggregate node.
const InternetNodeID = "internet"

type Link struct {
	ID         string
	A          string
	B          string
	LinkType   string // physical | wireless_association | logical | unknown
	Evidence   string // manual | lldp | cdp | wlc | inferred
	Confidence float64
	AInterface *string
	BInterface *string
}

type Network struct {
	Name   string
	CIDR   string
	VlanID *int
}

type Origin struct {
	Label     string
	Latitude  float64
	Longitude float64
	Precision string
}
