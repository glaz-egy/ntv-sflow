// Package enrichment holds independent enrichment layers: internal/external
// classification from configured CIDRs (D-008) and GeoIP/ASN metadata.
// Unknown stays unknown: lookups return nil/zero instead of guesses.
package enrichment

import (
	"fmt"
	"net/netip"
)

// Classifier decides internal vs external from configured prefixes
// (IPv4 and IPv6; never hard-coded RFC1918).
type Classifier struct {
	prefixes []netip.Prefix
}

func NewClassifier(cidrs []string) (*Classifier, error) {
	c := &Classifier{}
	for _, s := range cidrs {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("invalid internal CIDR %q: %w", s, err)
		}
		c.prefixes = append(c.prefixes, p.Masked())
	}
	return c, nil
}

// IsInternal reports (internal, ok). ok is false for unparseable addresses.
// IPv4-mapped IPv6 addresses are not unmapped, so they only match IPv6 prefixes.
func (c *Classifier) IsInternal(address string) (bool, bool) {
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return false, false
	}
	ip = ip.WithZone("")
	for _, p := range c.prefixes {
		if p.Contains(ip) {
			return true, true
		}
	}
	return false, true
}

// CanonicalIP returns a canonical textual key for address comparison, or "".
func CanonicalIP(address string) string {
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return ""
	}
	return ip.WithZone("").String()
}
