package enrichment

import (
	"fmt"
	"net/netip"
	"sort"
	"sync"

	"github.com/oschwald/maxminddb-golang/v2"
)

// GeoRecord is approximate GeoIP/ASN metadata for an external address (D-010).
// Every field may be unknown (nil).
type GeoRecord struct {
	CountryCode  *string
	CountryName  *string
	City         *string
	Latitude     *float64
	Longitude    *float64
	ASN          *int
	Organization *string
}

// GeoLookup resolves external addresses. Missing entries return the zero
// GeoRecord: unknown stays unknown, coordinates are never invented.
type GeoLookup interface {
	Lookup(ip string) GeoRecord
}

// MapGeo is a static lookup table keyed by exact address text (mock data).
type MapGeo map[string]GeoRecord

func (m MapGeo) Lookup(ip string) GeoRecord { return m[ip] }

// CountryAnchors are representative points for `country` grouping (D-022).
// Countries not listed fall back to their highest-traffic located member
// (basis `country_dominant`).
var CountryAnchors = map[string][2]float64{
	"JP": {36.2, 138.25}, "US": {39.5, -98.35}, "DE": {51.17, 10.45}, "NL": {52.13, 5.29},
	"SG": {1.35, 103.82}, "GB": {54.0, -2.0}, "AU": {-25.27, 133.78}, "BR": {-14.24, -51.93},
	"IN": {22.0, 79.0}, "KR": {36.5, 127.8}, "IE": {53.41, -8.24}, "SE": {62.0, 15.0},
	"FR": {46.6, 2.2}, "CA": {56.13, -106.35}, "HK": {22.32, 114.17},
}

// GeoOverride assigns metadata to a prefix (operator knowledge, demo data).
type GeoOverride struct {
	Prefix netip.Prefix
	Record GeoRecord
}

// OverrideGeo matches the most specific override prefix.
type OverrideGeo struct{ entries []GeoOverride }

func NewOverrideGeo(entries []GeoOverride) *OverrideGeo {
	sorted := append([]GeoOverride(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Prefix.Bits() > sorted[j].Prefix.Bits() })
	return &OverrideGeo{entries: sorted}
}

func (o *OverrideGeo) find(ip netip.Addr) (GeoRecord, bool) {
	for _, e := range o.entries {
		if e.Prefix.Contains(ip) {
			return e.Record, true
		}
	}
	return GeoRecord{}, false
}

// MaxMindGeo reads GeoLite2/GeoIP2 City and ASN databases (either optional).
type MaxMindGeo struct {
	city *maxminddb.Reader
	asn  *maxminddb.Reader
}

func OpenMaxMind(cityPath, asnPath string) (*MaxMindGeo, error) {
	m := &MaxMindGeo{}
	var err error
	if cityPath != "" {
		if m.city, err = maxminddb.Open(cityPath); err != nil {
			return nil, fmt.Errorf("open GeoIP city database %s: %w", cityPath, err)
		}
	}
	if asnPath != "" {
		if m.asn, err = maxminddb.Open(asnPath); err != nil {
			m.Close()
			return nil, fmt.Errorf("open GeoIP ASN database %s: %w", asnPath, err)
		}
	}
	return m, nil
}

func (m *MaxMindGeo) Close() {
	if m.city != nil {
		m.city.Close()
	}
	if m.asn != nil {
		m.asn.Close()
	}
}

type mmCity struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude  *float64 `maxminddb:"latitude"`
		Longitude *float64 `maxminddb:"longitude"`
	} `maxminddb:"location"`
}

type mmASN struct {
	Number       uint   `maxminddb:"autonomous_system_number"`
	Organization string `maxminddb:"autonomous_system_organization"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (m *MaxMindGeo) lookup(ip netip.Addr) GeoRecord {
	var g GeoRecord
	if m.city != nil {
		var rec mmCity
		if r := m.city.Lookup(ip); r.Found() && r.Decode(&rec) == nil {
			g.CountryCode = strPtr(rec.Country.ISOCode)
			g.CountryName = strPtr(rec.Country.Names["en"])
			g.City = strPtr(rec.City.Names["en"])
			if rec.Location.Latitude != nil && rec.Location.Longitude != nil {
				g.Latitude, g.Longitude = rec.Location.Latitude, rec.Location.Longitude
			}
		}
	}
	if m.asn != nil {
		var rec mmASN
		if r := m.asn.Lookup(ip); r.Found() && r.Decode(&rec) == nil && rec.Number != 0 {
			n := int(rec.Number)
			g.ASN = &n
			g.Organization = strPtr(rec.Organization)
		}
	}
	return g
}

// ChainGeo resolves via overrides first, then MaxMind, with a bounded cache.
// Lookups happen per window, off the collector hot path.
type ChainGeo struct {
	overrides *OverrideGeo
	maxmind   *MaxMindGeo
	mu        sync.Mutex
	cache     map[netip.Addr]GeoRecord
	limit     int
}

func NewChainGeo(overrides *OverrideGeo, maxmind *MaxMindGeo, cacheSize int) *ChainGeo {
	if overrides == nil {
		overrides = NewOverrideGeo(nil)
	}
	return &ChainGeo{overrides: overrides, maxmind: maxmind, cache: map[netip.Addr]GeoRecord{}, limit: cacheSize}
}

func (c *ChainGeo) Lookup(address string) GeoRecord {
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return GeoRecord{}
	}
	ip = ip.WithZone("")
	c.mu.Lock()
	if g, ok := c.cache[ip]; ok {
		c.mu.Unlock()
		return g
	}
	c.mu.Unlock()

	g, ok := c.overrides.find(ip)
	if !ok && c.maxmind != nil {
		g = c.maxmind.lookup(ip)
	}
	c.mu.Lock()
	if len(c.cache) >= c.limit {
		c.cache = map[netip.Addr]GeoRecord{} // simple bounded cache: reset when full
	}
	c.cache[ip] = g
	c.mu.Unlock()
	return g
}
