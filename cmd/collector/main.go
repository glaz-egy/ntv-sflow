// Command collector receives sFlow v5 datagrams over UDP and normalizes them
// into flow and interface-counter observations (Milestone C).
//
// Until aggregation is wired to the API (Milestone D), output is a periodic
// summary log, Prometheus metrics, and optional bounded JSON-line samples.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"network-traffic-visualizer/internal/collector"
	"network-traffic-visualizer/internal/config"
)

func main() {
	configPath := flag.String("config", os.Getenv("APP_CONFIG"), "path to config YAML (env APP_CONFIG)")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "collector")

	cfg, err := config.Load(*configPath, os.Getenv)
	if err != nil {
		log.Error("invalid configuration", "error", err.Error())
		os.Exit(1)
	}
	cc := cfg.Collector
	var allowed []netip.Prefix
	for _, s := range cc.AllowedSources {
		p, _ := config.ParseSource(s) // validated by Load
		allowed = append(allowed, p)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	summary := collector.NewSummarySink(log)
	sinks := collector.FanOut{summary}
	if cc.DebugFlowsPerSecond > 0 {
		log.Warn("debug flow output enabled: prints internal addresses and peers", "max_per_second", cc.DebugFlowsPerSecond)
		sinks = append(sinks, collector.NewDebugFlowSink(os.Stderr, cc.DebugFlowsPerSecond))
	}
	c := collector.New(collector.Options{
		Listen: cc.Listen, Workers: cc.Workers, QueueSize: cc.QueueSize,
		ReadBuffer: cc.ReadBufferBytes, AllowedSources: allowed, Logger: log,
	}, sinks)
	go summary.Run(ctx.Done(), cc.SummaryInterval)

	if cc.MetricsListen != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			c.Metrics().WritePrometheus(w)
		})
		mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
		srv := &http.Server{Addr: cc.MetricsListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("metrics server", "error", err.Error())
			}
		}()
		go func() {
			<-ctx.Done()
			srv.Close()
		}()
	}

	log.Info("collector listening", "udp", cc.Listen, "workers", cc.Workers, "queue_size", cc.QueueSize,
		"metrics", cc.MetricsListen, "allowed_sources", len(allowed))
	if err := c.Run(ctx); err != nil {
		log.Error("collector", "error", err.Error())
		os.Exit(1)
	}
	log.Info("collector stopped")
}
