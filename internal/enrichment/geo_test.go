package enrichment

import (
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// writeDB builds a tiny MaxMind DB with GeoLite2-compatible record layout.
func writeDB(t *testing.T, dbType string, records map[string]mmdbtype.Map) string {
	t.Helper()
	tree, err := mmdbwriter.New(mmdbwriter.Options{DatabaseType: dbType, RecordSize: 28, IncludeReservedNetworks: true})
	if err != nil {
		t.Fatal(err)
	}
	for cidr, rec := range records {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		if err := tree.Insert(n, rec); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), dbType+".mmdb")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := tree.WriteTo(f); err != nil {
		t.Fatal(err)
	}
	return path
}

func city(iso, country, cityName string, lat, lon float64) mmdbtype.Map {
	m := mmdbtype.Map{
		"country":  mmdbtype.Map{"iso_code": mmdbtype.String(iso), "names": mmdbtype.Map{"en": mmdbtype.String(country)}},
		"location": mmdbtype.Map{"latitude": mmdbtype.Float64(lat), "longitude": mmdbtype.Float64(lon)},
	}
	if cityName != "" {
		m["city"] = mmdbtype.Map{"names": mmdbtype.Map{"en": mmdbtype.String(cityName)}}
	}
	return m
}

func TestMaxMindAndOverrides(t *testing.T) {
	cityDB := writeDB(t, "GeoLite2-City", map[string]mmdbtype.Map{
		"198.51.100.0/24": city("JP", "Japan", "Tokyo", 35.68, 139.69),
		"203.0.113.0/24":  {"country": mmdbtype.Map{"iso_code": mmdbtype.String("DE"), "names": mmdbtype.Map{"en": mmdbtype.String("Germany")}}},
		"2001:db8::/32":   city("NL", "Netherlands", "Amsterdam", 52.37, 4.9),
	})
	asnDB := writeDB(t, "GeoLite2-ASN", map[string]mmdbtype.Map{
		"198.51.100.0/24": {"autonomous_system_number": mmdbtype.Uint32(64500), "autonomous_system_organization": mmdbtype.String("Example Org")},
	})
	mm, err := OpenMaxMind(cityDB, asnDB)
	if err != nil {
		t.Fatal(err)
	}
	defer mm.Close()

	overrides := NewOverrideGeo([]GeoOverride{
		{Prefix: netip.MustParsePrefix("198.51.100.0/24"), Record: GeoRecord{Organization: ptr("broad")}},
		{Prefix: netip.MustParsePrefix("198.51.100.77/32"), Record: GeoRecord{Organization: ptr("specific")}},
	})
	g := NewChainGeo(overrides, mm, 2)

	if r := g.Lookup("198.51.100.77"); r.Organization == nil || *r.Organization != "specific" {
		t.Fatalf("most specific override should win: %+v", r)
	}
	if r := g.Lookup("198.51.100.5"); *r.Organization != "broad" {
		t.Fatalf("override should beat MaxMind: %+v", r)
	}
	plain := NewChainGeo(nil, mm, 100)
	r := plain.Lookup("198.51.100.5")
	if *r.CountryCode != "JP" || *r.City != "Tokyo" || *r.Latitude != 35.68 || *r.ASN != 64500 || *r.Organization != "Example Org" {
		t.Fatalf("maxmind city+asn: %+v", r)
	}
	r = plain.Lookup("203.0.113.9")
	if *r.CountryCode != "DE" || r.City != nil || r.Latitude != nil || r.ASN != nil {
		t.Fatalf("country-only record must keep unknowns unknown: %+v", r)
	}
	if r = plain.Lookup("2001:db8::5"); *r.City != "Amsterdam" {
		t.Fatalf("IPv6: %+v", r)
	}
	if r = plain.Lookup("192.0.2.1"); r != (GeoRecord{}) {
		t.Fatalf("not found must be zero record: %+v", r)
	}
	if r = plain.Lookup("not-an-ip"); r != (GeoRecord{}) {
		t.Fatal("invalid address must be zero record")
	}
	for i := 0; i < 10; i++ { // cache stays bounded
		g.Lookup("198.51.100." + string(rune('0'+i)))
	}
	if len(g.cache) > 2 {
		t.Fatalf("cache exceeded limit: %d", len(g.cache))
	}
}

func TestOpenMaxMindMissingFile(t *testing.T) {
	if _, err := OpenMaxMind("/nonexistent/city.mmdb", ""); err == nil {
		t.Fatal("expected error")
	}
	m, err := OpenMaxMind("", "")
	if err != nil || NewChainGeo(nil, m, 10).Lookup("8.8.8.8") != (GeoRecord{}) {
		t.Fatal("no databases configured: everything unknown, no error")
	}
}

func ptr(s string) *string { return &s }
