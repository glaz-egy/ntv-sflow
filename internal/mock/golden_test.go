package mock

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"network-traffic-visualizer/internal/projection"
)

// Replays testdata/golden.json (written by the TypeScript mock, see
// apps/web/src/lib/mock-backend/golden.test.ts) against the Go port.

type goldenFile struct {
	EpochMs int64        `json:"epoch_ms"`
	Seed    int          `json:"seed"`
	Cases   []goldenCase `json:"cases"`
}

type goldenCase struct {
	Scenario string          `json:"scenario"`
	Tick     int             `json:"tick"`
	Request  goldenRequest   `json:"request"`
	Response json.RawMessage `json:"response"`
}

type goldenRequest struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	ID       string `json:"id"`
	Grouping string `json:"grouping"`
	Params   struct {
		Grouping        string   `json:"grouping"`
		SourceNodeID    string   `json:"source_node_id"`
		Direction       string   `json:"direction"`
		Protocol        string   `json:"protocol"`
		MinBps          float64  `json:"min_bps"`
		Limit           int      `json:"limit"`
		FocusNodeID     string   `json:"focus_node_id"`
		DestinationKey  string   `json:"destination_key"`
		VlanID          *int     `json:"vlan_id"`
		DeviceTypes     []string `json:"device_types"`
		IncludeInactive bool     `json:"include_inactive"`
		Scope           string   `json:"scope"`
	} `json:"params"`
}

func run(b *Backend, r goldenRequest) any {
	f, inv := b.Snapshot()
	p := r.Params
	switch r.Kind {
	case "status":
		return b.Status()
	case "globe":
		return inv.Globe(f, projection.GlobeQuery{
			Grouping: p.Grouping, SourceNodeID: p.SourceNodeID, Direction: p.Direction,
			Protocol: p.Protocol, MinBps: p.MinBps, Limit: p.Limit,
		})
	case "destination":
		if d := inv.Destination(f, r.Key, p.SourceNodeID, p.Protocol); d != nil {
			return d
		}
		return nil
	case "home":
		return inv.Home(f, projection.HomeQuery{
			FocusNodeID: p.FocusNodeID, DestinationKey: p.DestinationKey, VlanID: p.VlanID,
			DeviceTypes: p.DeviceTypes, Protocol: p.Protocol, MinBps: p.MinBps, Limit: p.Limit,
			IncludeInactive: p.IncludeInactive, Scope: p.Scope,
		})
	case "topology":
		return inv.Topology(f)
	case "device":
		if d := inv.Device(f, r.ID, r.Grouping); d != nil {
			return d
		}
		return nil
	}
	panic("unknown request kind " + r.Kind)
}

// diff returns the first difference between two decoded JSON values.
func diff(path string, want, got any) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: want object, got %T", path, got)
		}
		for k := range w {
			if _, ok := g[k]; !ok {
				return fmt.Sprintf("%s.%s: missing", path, k)
			}
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				return fmt.Sprintf("%s.%s: unexpected", path, k)
			}
		}
		for k, wv := range w {
			if d := diff(path+"."+k, wv, g[k]); d != "" {
				return d
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Sprintf("%s: want array len %d, got %v", path, len(w), lenOf(got))
		}
		for i := range w {
			if d := diff(fmt.Sprintf("%s[%d]", path, i), w[i], g[i]); d != "" {
				return d
			}
		}
	case float64:
		g, ok := got.(float64)
		if !ok || (g != w && math.Abs(g-w) > 1e-9*math.Max(math.Abs(w), 1)) {
			return fmt.Sprintf("%s: want %v, got %v", path, w, got)
		}
	default:
		if want != got {
			return fmt.Sprintf("%s: want %v, got %v", path, want, got)
		}
	}
	return ""
}

func lenOf(v any) any {
	if a, ok := v.([]any); ok {
		return len(a)
	}
	return v
}

func TestGoldenParityWithTypeScript(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var gf goldenFile
	if err := json.Unmarshal(raw, &gf); err != nil {
		t.Fatal(err)
	}
	epoch := time.UnixMilli(gf.EpochMs).UTC()
	backends := map[string]*Backend{}
	failures := 0
	for i, tc := range gf.Cases {
		key := fmt.Sprintf("%s@%d", tc.Scenario, tc.Tick)
		b, ok := backends[key]
		if !ok {
			b, err = NewBackend(Options{Seed: gf.Seed, Scenario: tc.Scenario, Speed: 1, Epoch: epoch})
			if err != nil {
				t.Fatal(err)
			}
			b.SetTick(tc.Tick)
			backends[key] = b
		}
		gotJSON, err := json.Marshal(run(b, tc.Request))
		if err != nil {
			t.Fatal(err)
		}
		var want, got any
		_ = json.Unmarshal(tc.Response, &want)
		_ = json.Unmarshal(gotJSON, &got)
		if d := diff("$", want, got); d != "" {
			failures++
			req, _ := json.Marshal(tc.Request)
			t.Errorf("case %d %s %s: %s", i, key, strings.TrimSpace(string(req)), d)
			if failures > 10 {
				t.Fatal("too many failures")
			}
		}
	}
	t.Logf("%d golden cases compared", len(gf.Cases))
}
