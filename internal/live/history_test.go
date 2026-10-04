package live

import (
	"context"
	"io"
	"log/slog"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"network-traffic-visualizer/internal/collector"
	"network-traffic-visualizer/internal/history"
	"network-traffic-visualizer/internal/mock"
	"network-traffic-visualizer/internal/projection"
	"network-traffic-visualizer/internal/sflowgen"
)

type storeSink struct {
	t     *testing.T
	store history.Store
}

func (s storeSink) RecordFlows(rows []history.FlowRow) {
	if err := s.store.WriteFlows(context.Background(), rows); err != nil {
		s.t.Fatal(err)
	}
}

func (s storeSink) RecordCounters(rows []history.CounterRow) {
	if err := s.store.WriteCounters(context.Background(), rows); err != nil {
		s.t.Fatal(err)
	}
}

// Seconds recorded by the live source rebuild the live window exactly: real
// sFlow → collector → live → history store → snapshot (D-059).
func TestLiveHistoryReproducesWindow(t *testing.T) {
	const T = mock.StartTick + 17
	store := history.NewMemory(history.MemoryOptions{Retention: 24 * time.Hour, Now: func() time.Time { return epoch.Add(time.Hour) }})
	src := demoSource(t, epoch)
	src.opts.History = storeSink{t, store}

	sc, _ := mock.BuildScenario("default", 42)
	engine, _ := mock.NewEngine(sc, 42)
	gen := sflowgen.New(engine)
	col := collector.New(collector.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, src)
	from := netip.MustParseAddrPort("10.0.0.1:6343")
	feed := func(from0, to int) {
		for sec := from0; sec <= to; sec++ {
			src.Advance(epoch.Add(time.Duration(sec) * time.Second))
			at := epoch.Add(time.Duration(sec)*time.Second + 400*time.Millisecond)
			for _, d := range gen.Second(sec, uint32(sec*1000)) {
				col.HandleDatagram(d, from, at)
			}
		}
	}
	feed(T-12, T)
	src.Advance(epoch.Add(time.Duration(T+1) * time.Second)) // window end = T
	lf, linv := src.Snapshot()
	start, end := lf.At(lf.WindowEndTick-lf.WindowSeconds), lf.At(lf.WindowEndTick)

	// Later seconds close the window's seconds and hand them to history.
	feed(T+1, T+mock.WindowSeconds+3)

	svc := history.NewService(store, src, func() time.Time { return epoch.Add(time.Duration(T+20) * time.Second) })
	snap, err := svc.Snapshot(context.Background(), start, end)
	if err != nil {
		t.Fatal(err)
	}
	hf, hinv := snap.Frame, snap.Inv
	if g := hinv.Globe(hf, projection.GlobeQuery{Grouping: "asn"}); len(g.Destinations) == 0 || g.Summary.Basis != "boundary_counter" {
		t.Fatalf("history snapshot is empty or lacks WAN counters: %d destinations, basis %s", len(g.Destinations), g.Summary.Basis)
	}
	check := func(name string, a, b any) {
		t.Helper()
		if x, y := normalize(t, a), normalize(t, b); !reflect.DeepEqual(x, y) {
			t.Errorf("%s differs\nlive:    %v\nhistory: %v", name, x, y)
		}
	}
	for _, g := range []string{"country", "asn", "ip"} {
		check("globe/"+g, linv.Globe(lf, projection.GlobeQuery{Grouping: g}), hinv.Globe(hf, projection.GlobeQuery{Grouping: g}))
	}
	check("home", linv.Home(lf, projection.HomeQuery{}), hinv.Home(hf, projection.HomeQuery{}))
	check("flows", linv.Flows(lf, projection.FlowQuery{Limit: 1000}), hinv.Flows(hf, projection.FlowQuery{Limit: 1000}))
}
