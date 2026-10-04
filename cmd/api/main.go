// Command api serves the REST + WebSocket API.
//
//	app.mode = mock: deterministic mock engine (same data as the in-browser mock, D-025)
//	app.mode = live: the sFlow collector runs in this process and feeds the
//	                 live aggregator (D-053); collector and aggregation stay
//	                 separate packages and can be split into processes later.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"network-traffic-visualizer/internal/collector"
	"network-traffic-visualizer/internal/config"
	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/httpapi"
	"network-traffic-visualizer/internal/inventory"
	"network-traffic-visualizer/internal/live"
	"network-traffic-visualizer/internal/mock"
	"network-traffic-visualizer/internal/realtime"
	"network-traffic-visualizer/internal/topology"
)

// source is what main needs from either backend.
type source interface {
	httpapi.Source
	Advance(now time.Time) bool
}

func main() {
	configPath := flag.String("config", os.Getenv("APP_CONFIG"), "path to config YAML (env APP_CONFIG)")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load(*configPath, os.Getenv)
	if err != nil {
		fatal(log, "invalid configuration", err)
	}
	origin := topology.Origin{
		Label: cfg.Network.Origin.Label, Latitude: cfg.Network.Origin.Latitude,
		Longitude: cfg.Network.Origin.Longitude, Precision: cfg.Network.Origin.Precision,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	now := time.Now()

	var src source
	var metrics func(io.Writer)
	switch cfg.App.Mode {
	case "mock":
		b, err := mock.NewBackend(mock.Options{
			Seed: cfg.Mock.Seed, Scenario: cfg.Mock.Scenario, Speed: cfg.Mock.Speed,
			Epoch: mock.EpochForStart(now), InternalCIDRs: cfg.Network.InternalCIDRs, Origin: &origin,
		})
		if err != nil {
			fatal(log, "mock backend", err)
		}
		log.Info("mock mode", "scenario", cfg.Mock.Scenario, "seed", cfg.Mock.Seed, "description", b.Description())
		src = b
	case "live":
		s, col, err := buildLive(ctx, cfg, origin, now, log)
		if err != nil {
			fatal(log, "live mode", err)
		}
		src = s
		metrics = func(w io.Writer) {
			col.Metrics().WritePrometheus(w)
			s.WritePrometheus(w)
		}
	}

	hub := realtime.NewHub(100)
	api := httpapi.New(src, hub, httpapi.Options{CORSAllowedOrigins: cfg.API.CORSAllowedOrigins, Logger: log, Metrics: metrics})
	srv := &http.Server{Addr: cfg.API.Listen, Handler: api, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 120 * time.Second}

	// One window_update per new aggregate window.
	go func() {
		t := time.NewTicker(cfg.App.UpdateInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if src.Advance(now) {
					hub.Broadcast(api.WindowUpdate())
				}
			}
		}
	}()
	go func() {
		log.Info("api listening", "addr", cfg.API.Listen, "mode", cfg.App.Mode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "error", err.Error())
			stop()
		}
	}()

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	log.Info("api stopped")
}

func buildLive(ctx context.Context, cfg config.Config, origin topology.Origin, now time.Time, log *slog.Logger) (*live.Source, *collector.Collector, error) {
	inv := inventory.Empty()
	if cfg.InventoryFile != "" {
		var err error
		if inv, err = inventory.Load(cfg.InventoryFile); err != nil {
			return nil, nil, err
		}
	} else {
		log.Warn("no inventory_file: internal endpoints will be shown as unresolved devices")
	}

	// GeoIP is optional: missing databases degrade to "Unknown location".
	optional := func(path, what string) string {
		if path == "" {
			return ""
		}
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			log.Warn("GeoIP database not found; locations will be unknown", "database", what, "path", path)
			return ""
		}
		return path
	}
	mm, err := enrichment.OpenMaxMind(optional(cfg.GeoIP.CityDBPath, "city"), optional(cfg.GeoIP.ASNDBPath, "asn"))
	if err != nil {
		return nil, nil, err
	}
	geo := enrichment.NewChainGeo(enrichment.NewOverrideGeo(inv.GeoOverrides), mm, 100_000)

	src, err := live.New(live.Options{
		WindowSeconds:  int(cfg.App.AggregationWindow.Seconds()),
		UpdateInterval: cfg.App.UpdateInterval,
		StaleAfter:     cfg.Live.StaleAfter,
		Epoch:          now.Truncate(time.Second),
		Inventory:      inv,
		InternalCIDRs:  cfg.Network.InternalCIDRs,
		Origin:         origin,
		Geo:            geo,
	})
	if err != nil {
		return nil, nil, err
	}

	cc := cfg.Collector
	var allowed []netip.Prefix
	for _, s := range cc.AllowedSources {
		p, _ := config.ParseSource(s)
		allowed = append(allowed, p)
	}
	summary := collector.NewSummarySink(log.With("component", "collector"))
	sinks := collector.FanOut{src, summary}
	if cc.DebugFlowsPerSecond > 0 {
		log.Warn("debug flow output enabled: prints internal addresses and peers", "max_per_second", cc.DebugFlowsPerSecond)
		sinks = append(sinks, collector.NewDebugFlowSink(os.Stderr, cc.DebugFlowsPerSecond))
	}
	col := collector.New(collector.Options{
		Listen: cc.Listen, Workers: cc.Workers, QueueSize: cc.QueueSize, ReadBuffer: cc.ReadBufferBytes,
		AllowedSources: allowed, Logger: log.With("component", "collector"),
	}, sinks)
	// Fail fast if the UDP socket cannot be bound (e.g. port in use): a live
	// API without its collector would only ever show "Waiting for sFlow".
	runErr := make(chan error, 1)
	go func() { runErr <- col.Run(ctx) }()
	bindCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrReady := make(chan error, 1)
	go func() { _, err := col.Addr(bindCtx); addrReady <- err }()
	select {
	case err := <-runErr:
		if err == nil {
			err = errors.New("collector stopped during startup")
		}
		return nil, nil, fmt.Errorf("sFlow collector on %s: %w", cc.Listen, err)
	case err := <-addrReady:
		if err != nil {
			return nil, nil, fmt.Errorf("sFlow collector on %s did not start: %w", cc.Listen, err)
		}
	}
	go func() {
		if err := <-runErr; err != nil {
			log.Error("collector", "error", err.Error())
		}
	}()
	go summary.Run(ctx.Done(), cc.SummaryInterval)
	log.Info("live mode", "sflow_udp", cc.Listen, "devices", len(inv.Devices), "exporters", len(inv.Exporters),
		"geo_overrides", len(inv.GeoOverrides), "metrics", "/metrics on the API")
	return src, col, nil
}

func fatal(log *slog.Logger, msg string, err error) {
	log.Error(msg, "error", err.Error())
	os.Exit(1)
}
