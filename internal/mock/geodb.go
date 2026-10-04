package mock

import "network-traffic-visualizer/internal/enrichment"

// Mock GeoIP/ASN data (docs/MOCK_DATA.md §4). Documentation-range IPs;
// org labels are illustrative. Mirrors geodb.ts.

type City struct {
	Name        string
	CountryCode string
	Latitude    float64
	Longitude   float64
}

var CountryNames = map[string]string{
	"JP": "Japan", "US": "United States", "DE": "Germany", "NL": "Netherlands", "SG": "Singapore",
	"GB": "United Kingdom", "AU": "Australia", "BR": "Brazil", "IN": "India", "KR": "South Korea",
	"IE": "Ireland", "SE": "Sweden", "FR": "France", "CA": "Canada", "HK": "Hong Kong",
}

// CountryAnchors are shared with live mode (enrichment.CountryAnchors).
var CountryAnchors = enrichment.CountryAnchors

var Cities = []City{
	{"Tokyo", "JP", 35.68, 139.69},
	{"Osaka", "JP", 34.69, 135.5},
	{"San Jose", "US", 37.34, -121.89},
	{"Ashburn", "US", 39.04, -77.49},
	{"Council Bluffs", "US", 41.26, -95.86},
	{"Seattle", "US", 47.61, -122.33},
	{"Los Angeles", "US", 34.05, -118.24},
	{"Frankfurt", "DE", 50.11, 8.68},
	{"Amsterdam", "NL", 52.37, 4.9},
	{"Singapore", "SG", 1.35, 103.82},
	{"London", "GB", 51.51, -0.13},
	{"Sydney", "AU", -33.87, 151.21},
	{"Sao Paulo", "BR", -23.55, -46.63},
	{"Mumbai", "IN", 19.08, 72.88},
	{"Seoul", "KR", 37.57, 126.98},
	{"Dublin", "IE", 53.35, -6.26},
	{"Stockholm", "SE", 59.33, 18.07},
	{"Paris", "FR", 48.86, 2.35},
	{"Toronto", "CA", 43.65, -79.38},
	{"Hong Kong", "HK", 22.32, 114.17},
}

type Org struct {
	ASN          int
	Organization string
}

var Orgs = []Org{
	{13335, "Cloudflare"}, {15169, "Google"}, {16509, "Amazon"}, {8075, "Microsoft"},
	{20940, "Akamai"}, {2906, "Netflix"}, {32590, "Valve"},
}

func fp(f float64) *float64 { return &f }

func cityGeo(c City, asn int, org string) enrichment.GeoRecord {
	g := enrichment.GeoRecord{
		CountryCode: sp(c.CountryCode), City: sp(c.Name),
		Latitude: fp(c.Latitude), Longitude: fp(c.Longitude), ASN: ip(asn), Organization: sp(org),
	}
	if n, ok := CountryNames[c.CountryCode]; ok {
		g.CountryName = sp(n)
	}
	return g
}

func cityRecord(name string, asn int, org string) enrichment.GeoRecord {
	for _, c := range Cities {
		if c.Name == name {
			return cityGeo(c, asn, org)
		}
	}
	panic("unknown mock city " + name)
}

func BaseGeoDB() enrichment.MapGeo {
	return enrichment.MapGeo{
		"198.51.100.10":  cityRecord("Tokyo", 13335, "Cloudflare"),
		"198.51.100.11":  cityRecord("San Jose", 13335, "Cloudflare"),
		"2001:db8:1::10": cityRecord("Frankfurt", 13335, "Cloudflare"),
		"203.0.113.20":   cityRecord("Tokyo", 15169, "Google"),
		"203.0.113.21":   cityRecord("Council Bluffs", 15169, "Google"),
		"192.0.2.30":     cityRecord("Ashburn", 16509, "Amazon"),
		"192.0.2.31":     cityRecord("Singapore", 16509, "Amazon"),
		"203.0.113.40":   cityRecord("Osaka", 8075, "Microsoft"),
		"203.0.113.41":   cityRecord("Amsterdam", 8075, "Microsoft"),
		"198.51.100.50":  cityRecord("Tokyo", 20940, "Akamai"),
		"203.0.113.80":   cityRecord("Tokyo", 2906, "Netflix"),
		"203.0.113.60":   cityRecord("Tokyo", 32590, "Valve"),
		// ASN known, location unknown (documentation ASN range).
		"198.51.100.200": {ASN: ip(64500), Organization: sp("Example Transit (mock)")},
		// 192.0.2.250 intentionally absent: fully unknown metadata.
	}
}
