// Command sflow-gen sends real sFlow v5 datagrams generated from the
// deterministic mock traffic, for testing the collector without hardware.
//
//	go run ./cmd/sflow-gen -target 127.0.0.1:6343 -scenario default
package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"network-traffic-visualizer/internal/mock"
	"network-traffic-visualizer/internal/sflowgen"
)

func main() {
	target := flag.String("target", "127.0.0.1:6343", "collector UDP address")
	scenario := flag.String("scenario", "default", "mock scenario")
	seed := flag.Int("seed", 42, "mock seed")
	speed := flag.Float64("speed", 1, "sim seconds per real second")
	duration := flag.Duration("duration", 0, "stop after this long (0 = until interrupted)")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if *speed <= 0 || *speed > 100 {
		log.Error("-speed must be in (0, 100]")
		os.Exit(2)
	}
	sc, err := mock.BuildScenario(*scenario, *seed)
	if err != nil {
		log.Error("scenario", "error", err.Error())
		os.Exit(2)
	}
	engine, err := mock.NewEngine(sc, *seed)
	if err != nil {
		log.Error("engine", "error", err.Error())
		os.Exit(1)
	}
	gen := sflowgen.New(engine)
	conn, err := net.Dial("udp", *target)
	if err != nil {
		log.Error("dial", "error", err.Error())
		os.Exit(1)
	}
	defer conn.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}

	log.Info("sending sFlow v5", "target", *target, "scenario", *scenario, "seed", *seed, "speed", *speed)
	start := time.Now()
	ticker := time.NewTicker(time.Duration(float64(time.Second) / *speed))
	defer ticker.Stop()
	tick, sent, bytes := mock.StartTick, 0, 0
	lastLog := start
	for {
		uptime := uint32(time.Since(start).Milliseconds()) + 60_000
		for _, d := range gen.Second(tick, uptime) {
			if _, err := conn.Write(d); err != nil {
				log.Warn("send failed", "error", err.Error())
				continue
			}
			sent++
			bytes += len(d)
		}
		tick++
		if time.Since(lastLog) >= 10*time.Second {
			log.Info("progress", "sim_tick", tick, "datagrams", sent, "bytes", bytes)
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			log.Info("done", "datagrams", sent, "bytes", bytes)
			return
		case <-ticker.C:
		}
	}
}
