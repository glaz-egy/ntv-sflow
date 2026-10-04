package httpapi

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// params validates query parameters and collects the first error.
type params struct {
	q   url.Values
	err error
}

func (p *params) fail(format string, a ...any) {
	if p.err == nil {
		p.err = fmt.Errorf(format, a...)
	}
}

func (p *params) str(name string) string { return strings.TrimSpace(p.q.Get(name)) }

func (p *params) enum(name string, required bool, allowed ...string) string {
	v := p.str(name)
	if v == "" {
		if required {
			p.fail("%s is required (one of %s)", name, strings.Join(allowed, ", "))
		}
		return ""
	}
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	p.fail("%s must be one of %s", name, strings.Join(allowed, ", "))
	return ""
}

func (p *params) float(name string, min float64) float64 {
	v := p.str(name)
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < min {
		p.fail("%s must be a number >= %v", name, min)
		return 0
	}
	return f
}

func (p *params) intPtr(name string, min, max int) *int {
	v := p.str(name)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		p.fail("%s must be an integer in [%d, %d]", name, min, max)
		return nil
	}
	return &n
}

func (p *params) int(name string, min, max int) int {
	if v := p.intPtr(name, min, max); v != nil {
		return *v
	}
	return 0
}

func (p *params) boolean(name string) bool {
	v := p.str(name)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.fail("%s must be true or false", name)
	}
	return b
}

func (p *params) list(name string, allowed ...string) []string {
	v := p.str(name)
	if v == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		item = strings.TrimSpace(item)
		ok := false
		for _, a := range allowed {
			ok = ok || a == item
		}
		if !ok {
			p.fail("%s: unknown value %q", name, item)
			return nil
		}
		out = append(out, item)
	}
	return out
}

var (
	groupings   = []string{"country", "city", "asn", "ip"}
	protocols   = []string{"tcp", "udp", "icmp", "other"}
	deviceTypes = []string{"internet", "router", "firewall", "switch", "wireless_ap", "server", "pc", "smartphone", "iot", "vm", "kubernetes_node", "unknown"}
)
