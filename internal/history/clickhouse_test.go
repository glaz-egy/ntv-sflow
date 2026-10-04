package history

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Integration test against a real ClickHouse (skipped without one):
//
//	docker run -d --rm -p 8123:8123 -e CLICKHOUSE_SKIP_USER_SETUP=1 clickhouse/clickhouse-server
//	NTV_TEST_CLICKHOUSE_URL=http://localhost:8123 go test ./internal/history/ -run ClickHouse
func openTestClickHouse(t *testing.T) *ClickHouse {
	t.Helper()
	base := os.Getenv("NTV_TEST_CLICKHOUSE_URL")
	if base == "" {
		t.Skip("NTV_TEST_CLICKHOUSE_URL not set")
	}
	db := fmt.Sprintf("ntv_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	c, err := OpenClickHouse(ctx, base+"/"+db, ClickHouseOptions{
		RawRetention: 3650 * 24 * time.Hour, MinuteRetention: 3650 * 24 * time.Hour, HourRetention: 3650 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.DropDatabase(context.Background())
		c.Close()
	})
	return c
}

func TestClickHouseStoreContract(t *testing.T) {
	storeContract(t, openTestClickHouse(t))
}

func TestClickHouseRollupsAndMigrationsAreIdempotent(t *testing.T) {
	c := openTestClickHouse(t)
	ctx := context.Background()
	// Re-opening applies no migration twice and keeps data.
	if err := c.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var rows []FlowRow
	for sec := 0; sec < 180; sec++ {
		rows = append(rows, row(sec, "edge", "192.168.1.10", "203.0.113.5", 50000, 443, 1, 100, true, false))
	}
	if err := c.WriteFlows(ctx, rows); err != nil {
		t.Fatal(err)
	}
	raw, minute, hour := c.Tiers()[0], c.Tiers()[1], c.Tiers()[2]
	for _, tier := range []Tier{raw, minute, hour} {
		got, err := c.Flows(ctx, tier, t0, t0.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Obs.SampleCount != 180 || got[0].Obs.EstimatedBytes != 18000 {
			t.Fatalf("tier %v: %+v", tier.Step, got)
		}
		// Rollups keep the newest raw second, not the bucket start.
		if !got[0].LastStart.Equal(t0.Add(179 * time.Second)) {
			t.Errorf("tier %v last start %v", tier.Step, got[0].LastStart)
		}
	}
	tl, err := c.Timeline(ctx, minute, t0, t0.Add(3*time.Minute), time.Minute, Dedup{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tl) != 3 || tl[1].OutboundBytes != 6000 {
		t.Fatalf("minute timeline: %+v", tl)
	}
}

// Recording that starts mid-hour: rollup buckets (whole minute/hour) must
// not make history look older than it is.
func TestClickHouseCoverageUsesFinestTable(t *testing.T) {
	c := openTestClickHouse(t)
	ctx := context.Background()
	if err := c.WriteFlows(ctx, []FlowRow{
		row(95, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1, true, false),
		row(130, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1, true, false),
	}); err != nil {
		t.Fatal(err)
	}
	e, l, ok, err := c.Coverage(ctx)
	if err != nil || !ok || !e.Equal(t0.Add(95*time.Second)) || !l.Equal(t0.Add(130*time.Second)) {
		t.Errorf("coverage %v..%v ok=%v err=%v", e, l, ok, err)
	}
}
