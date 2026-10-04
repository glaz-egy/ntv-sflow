package aggregation

import (
	"sort"
	"strconv"
	"strings"

	"network-traffic-visualizer/internal/enrichment"
)

var Groupings = []string{"country", "city", "asn", "ip"}

func IsGrouping(s string) bool {
	for _, g := range Groupings {
		if g == s {
			return true
		}
	}
	return false
}

// DestinationKey returns `<grouping>:<value>` for an external IP.
func DestinationKey(grouping, ip string, g enrichment.GeoRecord) string {
	switch grouping {
	case "ip":
		return "ip:" + ip
	case "asn":
		if g.ASN != nil {
			return "asn:" + strconv.Itoa(*g.ASN)
		}
		return "asn:unknown"
	case "country":
		if g.CountryCode != nil {
			return "country:" + *g.CountryCode
		}
		return "country:unknown"
	case "city":
		if g.CountryCode != nil && g.City != nil {
			return "city:" + *g.CountryCode + ":" + *g.City
		}
		return "city:unknown"
	}
	return ""
}

// ParseDestinationKey splits on the first colon only.
func ParseDestinationKey(key string) (grouping, value string, ok bool) {
	i := strings.IndexByte(key, ':')
	if i <= 0 || i == len(key)-1 {
		return "", "", false
	}
	grouping, value = key[:i], key[i+1:]
	if !IsGrouping(grouping) {
		return "", "", false
	}
	return grouping, value, true
}

func DestinationLabel(grouping, ip string, g enrichment.GeoRecord) string {
	switch grouping {
	case "ip":
		return ip
	case "asn":
		if g.ASN == nil {
			return "Unknown ASN"
		}
		if g.Organization != nil {
			return *g.Organization
		}
		return "AS" + strconv.Itoa(*g.ASN)
	case "country":
		if g.CountryName != nil {
			return *g.CountryName
		}
		if g.CountryCode != nil {
			return *g.CountryCode
		}
		return "Unknown country"
	case "city":
		if g.City != nil && g.CountryCode != nil {
			return *g.City + ", " + *g.CountryCode
		}
		return "Unknown city"
	}
	return ip
}

type Location struct {
	Latitude, Longitude float64
	Basis               string
}

type WeightedGeo struct {
	Geo    enrichment.GeoRecord
	Weight float64
}

// GroupLocation returns the marker anchor for a group (D-022) or nil when
// unknown. Callers must not substitute a placeholder.
func GroupLocation(grouping string, members []WeightedGeo, countryAnchors map[string][2]float64) *Location {
	if len(members) == 0 {
		return nil
	}
	first := members[0].Geo
	switch grouping {
	case "ip", "city":
		if first.Latitude == nil || first.Longitude == nil {
			return nil
		}
		basis := "city"
		if grouping == "ip" {
			basis = "geoip"
		}
		return &Location{*first.Latitude, *first.Longitude, basis}
	case "country":
		if first.CountryCode == nil {
			return nil
		}
		if a, ok := countryAnchors[*first.CountryCode]; ok {
			return &Location{a[0], a[1], "country_anchor"}
		}
		// No configured anchor: use the highest-traffic located member.
		if loc := dominantLocation(members); loc != nil {
			loc.Basis = "country_dominant"
			return loc
		}
		return nil
	case "asn":
		if loc := dominantLocation(members); loc != nil {
			loc.Basis = "asn_dominant"
			return loc
		}
		return nil
	}
	return nil
}

// dominantLocation returns the member location with the most traffic
// (ties broken by coordinate text, matching the TypeScript reference).
func dominantLocation(members []WeightedGeo) *Location {
	type acc struct{ lat, lon, w float64 }
	weights := map[string]*acc{}
	for _, m := range members {
		if m.Geo.Latitude == nil || m.Geo.Longitude == nil {
			continue
		}
		// Key text matches JS number formatting for the coordinates used.
		k := strconv.FormatFloat(*m.Geo.Latitude, 'f', -1, 64) + "," + strconv.FormatFloat(*m.Geo.Longitude, 'f', -1, 64)
		e, ok := weights[k]
		if !ok {
			e = &acc{lat: *m.Geo.Latitude, lon: *m.Geo.Longitude}
			weights[k] = e
		}
		e.w += m.Weight
	}
	keys := make([]string, 0, len(weights))
	for k := range weights {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var best *acc
	for _, k := range keys {
		if e := weights[k]; best == nil || e.w > best.w {
			best = e
		}
	}
	if best == nil {
		return nil
	}
	return &Location{Latitude: best.lat, Longitude: best.lon}
}
