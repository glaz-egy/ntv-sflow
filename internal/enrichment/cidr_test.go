package enrichment

import "testing"

func TestClassifier(t *testing.T) {
	c, err := NewClassifier([]string{"192.168.0.0/16", "203.0.113.0/24", "2001:db8:42::/48"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		ip       string
		internal bool
		ok       bool
	}{
		{"192.168.1.10", true, true},
		{"203.0.113.77", true, true}, // operator-owned public prefix
		{"10.0.0.1", false, true},    // RFC1918 but not configured
		{"2001:db8:42::5", true, true},
		{"2001:db8:43::5", false, true},
		{"fe80::1%eth0", false, true},
		{"::ffff:192.168.1.1", false, true}, // no cross-family matching
		{"not-an-ip", false, false},
		{"10.0.0.256", false, false},
	}
	for _, tc := range cases {
		internal, ok := c.IsInternal(tc.ip)
		if internal != tc.internal || ok != tc.ok {
			t.Errorf("%s: got (%v,%v) want (%v,%v)", tc.ip, internal, ok, tc.internal, tc.ok)
		}
	}
}

func TestClassifierRejectsInvalidConfig(t *testing.T) {
	if _, err := NewClassifier([]string{"10.0.0.0/33"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestCanonicalIP(t *testing.T) {
	if CanonicalIP("2001:0db8:0000::0001") != CanonicalIP("2001:db8::1") {
		t.Fatal("IPv6 forms should canonicalise equally")
	}
	if CanonicalIP("bogus") != "" {
		t.Fatal("invalid address should give empty key")
	}
}
