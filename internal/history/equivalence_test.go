package history_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"network-traffic-visualizer/internal/history"
	"network-traffic-visualizer/internal/mock"
	"network-traffic-visualizer/internal/projection"
)

// syncSink writes straight to a store (no recorder goroutine) for tests.
type syncSink struct {
	t     *testing.T
	store history.Store
}

func (s syncSink) RecordFlows(rows []history.FlowRow) {
	if err := s.store.WriteFlows(context.Background(), rows); err != nil {
		s.t.Fatal(err)
	}
}

func (s syncSink) RecordCounters(rows []history.CounterRow) {
	if err := s.store.WriteCounters(context.Background(), rows); err != nil {
		s.t.Fatal(err)
	}
}

// asJSON drops server_time: live reports the clock (it keeps running when
// data stops, e.g. stale-collector); history reports the window end.
func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if obj, ok := m.(map[string]any); ok {
		delete(obj, "server_time")
	}
	b, _ = json.Marshal(m)
	return string(b)
}

// A historical snapshot of the seconds a live window covered must project
// to exactly the same Globe/Home/Flows/Device responses: history stores the
// pre-attribution input and reuses the unchanged pipeline (D-059).
func TestHistoricalSnapshotEqualsLiveWindow(t *testing.T) {
	epoch := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	equivalence(t, epoch, func(t *testing.T) history.Store {
		return history.NewMemory(history.MemoryOptions{Retention: 24 * time.Hour, Now: func() time.Time { return epoch.Add(time.Hour) }})
	})
}

// Same through ClickHouse (IP round-trips, server-side merge order).
func TestHistoricalSnapshotEqualsLiveWindowClickHouse(t *testing.T) {
	base := os.Getenv("NTV_TEST_CLICKHOUSE_URL")
	if base == "" {
		t.Skip("NTV_TEST_CLICKHOUSE_URL not set")
	}
	epoch := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	equivalence(t, epoch, func(t *testing.T) history.Store {
		year := 365 * 24 * time.Hour
		c, err := history.OpenClickHouse(context.Background(), fmt.Sprintf("%s/ntv_eq_%d", base, time.Now().UnixNano()),
			history.ClickHouseOptions{RawRetention: year, MinuteRetention: year, HourRetention: year})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.DropDatabase(context.Background()) })
		return c
	})
}

func equivalence(t *testing.T, epoch time.Time, newStore func(*testing.T) history.Store) {
	for _, scenario := range mock.ScenarioNames {
		t.Run(scenario, func(t *testing.T) {
			store := newStore(t)
			b, err := mock.NewBackend(mock.Options{Seed: 42, Scenario: scenario, Speed: 1, Epoch: epoch, History: syncSink{t, store}})
			if err != nil {
				t.Fatal(err)
			}
			svc := history.NewService(store, b, b.Now)

			for tick := mock.StartTick + 1; tick <= mock.StartTick+40; tick += 7 {
				b.Advance(epoch.Add(time.Duration(tick) * time.Second))
				live, inv := b.Snapshot()
				start := live.At(live.WindowEndTick - live.WindowSeconds)
				end := live.At(live.WindowEndTick)
				snap, err := svc.Snapshot(context.Background(), start, end)
				if err != nil {
					t.Fatal(err)
				}
				h, hinv := snap.Frame, snap.Inv

				compare := func(name string, a, b any) {
					t.Helper()
					if x, y := asJSON(t, a), asJSON(t, b); x != y {
						t.Fatalf("tick %d %s differs\nlive:    %s\nhistory: %s", tick, name, x, y)
					}
				}
				for _, g := range []string{"country", "city", "asn", "ip"} {
					compare("globe/"+g, inv.Globe(live, projection.GlobeQuery{Grouping: g}), hinv.Globe(h, projection.GlobeQuery{Grouping: g}))
				}
				compare("home", inv.Home(live, projection.HomeQuery{}), hinv.Home(h, projection.HomeQuery{}))
				compare("home/inactive", inv.Home(live, projection.HomeQuery{IncludeInactive: true}), hinv.Home(h, projection.HomeQuery{IncludeInactive: true}))
				compare("flows", inv.Flows(live, projection.FlowQuery{Limit: 1000}), hinv.Flows(h, projection.FlowQuery{Limit: 1000}))
				for _, d := range inv.Globe(live, projection.GlobeQuery{Grouping: "asn"}).Destinations {
					compare("destination "+d.Key, inv.Destination(live, d.Key, "", ""), hinv.Destination(h, d.Key, "", ""))
				}
				for _, n := range inv.Home(live, projection.HomeQuery{}).Nodes {
					compare("device "+n.ID, inv.Device(live, n.ID, "asn"), hinv.Device(h, n.ID, "asn"))
				}
			}
		})
	}
}
