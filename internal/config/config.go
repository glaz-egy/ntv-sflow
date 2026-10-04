// Package config loads runtime configuration: defaults → YAML → environment
// (docs/DEVELOPMENT.md §6), then validates it. Secrets (DSNs) should come
// from the environment, not committed YAML.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"network-traffic-visualizer/internal/history"
	"network-traffic-visualizer/internal/mock"
)

type Config struct {
	App App `yaml:"app"`
	// InventoryFile describes devices, exporters, topology and GeoIP
	// overrides (configs/inventory.example.yaml). Optional.
	InventoryFile string        `yaml:"inventory_file"`
	Live          Live          `yaml:"live"`
	API           API           `yaml:"api"`
	Mock          Mock          `yaml:"mock"`
	Collector     Collector     `yaml:"collector"`
	Network       Network       `yaml:"network"`
	Visualization Visualization `yaml:"visualization"`
	GeoIP         GeoIP         `yaml:"geoip"`
	Postgres      DSN           `yaml:"postgres"`
	ClickHouse    DSN           `yaml:"clickhouse"`
	Redis         Redis         `yaml:"redis"`
	History       History       `yaml:"history"`
}

type App struct {
	Mode              string        `yaml:"mode"`
	UpdateInterval    time.Duration `yaml:"update_interval"`
	AggregationWindow time.Duration `yaml:"aggregation_window"`
}

type Live struct {
	// StaleAfter: no sFlow datagram for this long marks data stale.
	StaleAfter time.Duration `yaml:"stale_after"`
}

type API struct {
	Listen             string   `yaml:"listen"`
	CORSAllowedOrigins []string `yaml:"cors_allowed_origins"`
}

type Mock struct {
	Seed     int     `yaml:"seed"`
	Scenario string  `yaml:"scenario"`
	Speed    float64 `yaml:"speed"`
}

type Collector struct {
	Listen   string `yaml:"listen"`
	Protocol string `yaml:"protocol"`
	// Workers decode datagrams in parallel; QueueSize bounds buffered datagrams.
	Workers   int `yaml:"workers"`
	QueueSize int `yaml:"queue_size"`
	// ReadBufferBytes sets SO_RCVBUF (0 = OS default).
	ReadBufferBytes int `yaml:"read_buffer_bytes"`
	// AllowedSources limits accepted UDP source addresses (empty = accept all).
	AllowedSources []string `yaml:"allowed_sources"`
	// MetricsListen serves /metrics and /healthz ("" disables).
	MetricsListen   string        `yaml:"metrics_listen"`
	SummaryInterval time.Duration `yaml:"summary_interval"`
	// DebugFlowsPerSecond > 0 prints normalized samples as JSON lines.
	// Sensitive (internal addresses, peers): keep 0 in production.
	DebugFlowsPerSecond int `yaml:"debug_flows_per_second"`
}

type Network struct {
	InternalCIDRs []string `yaml:"internal_cidrs"`
	Origin        Origin   `yaml:"origin"`
}

type Origin struct {
	Label     string  `yaml:"label"`
	Latitude  float64 `yaml:"latitude"`
	Longitude float64 `yaml:"longitude"`
	Precision string  `yaml:"precision"`
}

type Visualization struct {
	Globe struct {
		DefaultGrouping string  `yaml:"default_grouping"`
		MaxArcs         int     `yaml:"max_arcs"`
		MinBps          float64 `yaml:"min_bps"`
		Particles       bool    `yaml:"particles"`
	} `yaml:"globe"`
	Home struct {
		MaxNodes    int     `yaml:"max_nodes"`
		MaxEdges    int     `yaml:"max_edges"`
		MinBps      float64 `yaml:"min_bps"`
		DefaultMode string  `yaml:"default_mode"`
	} `yaml:"home"`
}

type GeoIP struct {
	CityDBPath string `yaml:"city_db_path"`
	ASNDBPath  string `yaml:"asn_db_path"`
}

type DSN struct {
	DSN string `yaml:"dsn"`
}

type Redis struct {
	Enabled bool   `yaml:"enabled"`
	Address string `yaml:"address"`
}

// History configures persistence (Milestone E, D-059).
type History struct {
	// Backend: none | memory (bounded, lost on restart) | clickhouse
	// (clickhouse.dsn, an HTTP URL such as http://localhost:8123/ntv).
	Backend string `yaml:"backend"`
	// MemoryRetention bounds the memory backend (e.g. "1h").
	MemoryRetention string `yaml:"memory_retention"`
	// ClickHouse retention per resolution: per-second rows, 1-minute and
	// 1-hour rollups ("7d", "90d", "365d"; Go durations also accepted).
	RawFlowRetention     string `yaml:"raw_flow_retention"`
	Aggregate1mRetention string `yaml:"aggregate_1m_retention"`
	Aggregate1hRetention string `yaml:"aggregate_1h_retention"`
}

func Defaults() Config {
	var c Config
	c.App = App{Mode: "mock", UpdateInterval: time.Second, AggregationWindow: 5 * time.Second}
	c.Live = Live{StaleAfter: 15 * time.Second}
	c.API = API{Listen: ":8080", CORSAllowedOrigins: []string{"http://localhost:3000"}}
	c.Mock = Mock{Seed: 42, Scenario: "default", Speed: 1}
	c.Collector = Collector{
		Listen: "0.0.0.0:6343", Protocol: "sflow-v5", Workers: 2, QueueSize: 4096,
		MetricsListen: ":9102", SummaryInterval: 10 * time.Second,
	}
	c.Network = Network{
		InternalCIDRs: append([]string(nil), mock.DefaultInternalCIDRs...),
		Origin:        Origin{Label: "Home Network", Latitude: 35.68, Longitude: 139.76, Precision: "city"},
	}
	c.Visualization.Globe.DefaultGrouping = "asn"
	c.Visualization.Globe.MaxArcs = 100
	c.Visualization.Globe.Particles = true
	c.Visualization.Home.MaxNodes = 300
	c.Visualization.Home.MaxEdges = 250
	c.Visualization.Home.DefaultMode = "traffic"
	c.History = History{Backend: "memory", MemoryRetention: "1h",
		RawFlowRetention: "7d", Aggregate1mRetention: "90d", Aggregate1hRetention: "365d"}
	return c
}

// Load reads defaults, then the YAML file (if path != ""), then env overrides.
func Load(path string, getenv func(string) string) (Config, error) {
	c := Defaults()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("read config: %w", err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true) // typos in config keys are errors, not silent defaults
		if err := dec.Decode(&c); err != nil {
			return c, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if err := applyEnv(&c, getenv); err != nil {
		return c, err
	}
	return c, c.Validate()
}

func applyEnv(c *Config, getenv func(string) string) error {
	str := func(key string, dst *string) {
		if v := getenv(key); v != "" {
			*dst = v
		}
	}
	str("APP_MODE", &c.App.Mode)
	str("INVENTORY_FILE", &c.InventoryFile)
	str("COLLECTOR_LISTEN", &c.Collector.Listen)
	str("COLLECTOR_METRICS_LISTEN", &c.Collector.MetricsListen)
	if v := getenv("COLLECTOR_ALLOWED_SOURCES"); v != "" {
		c.Collector.AllowedSources = splitList(v)
	}
	if v := getenv("COLLECTOR_DEBUG_FLOWS_PER_SECOND"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("COLLECTOR_DEBUG_FLOWS_PER_SECOND: %w", err)
		}
		c.Collector.DebugFlowsPerSecond = n
	}
	str("API_LISTEN", &c.API.Listen)
	str("MOCK_SCENARIO", &c.Mock.Scenario)
	str("POSTGRES_DSN", &c.Postgres.DSN)
	str("CLICKHOUSE_DSN", &c.ClickHouse.DSN)
	str("HISTORY_BACKEND", &c.History.Backend)
	str("REDIS_ADDRESS", &c.Redis.Address)
	str("GEOIP_CITY_DB", &c.GeoIP.CityDBPath)
	str("GEOIP_ASN_DB", &c.GeoIP.ASNDBPath)
	if v := getenv("API_CORS_ALLOWED_ORIGINS"); v != "" {
		c.API.CORSAllowedOrigins = splitList(v)
	}
	if v := getenv("NETWORK_INTERNAL_CIDRS"); v != "" {
		c.Network.InternalCIDRs = splitList(v)
	}
	if v := getenv("MOCK_SEED"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("MOCK_SEED: %w", err)
		}
		c.Mock.Seed = n
	}
	if v := getenv("MOCK_SPEED"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("MOCK_SPEED: %w", err)
		}
		c.Mock.Speed = f
	}
	if v := getenv("REDIS_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("REDIS_ENABLED: %w", err)
		}
		c.Redis.Enabled = b
	}
	return nil
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Validate reports every problem at once.
func (c Config) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	switch c.App.Mode {
	case "mock", "live":
	default:
		add("app.mode: must be mock or live, got %q", c.App.Mode)
	}
	if c.App.UpdateInterval <= 0 {
		add("app.update_interval: must be > 0")
	}
	if c.App.AggregationWindow < c.App.UpdateInterval {
		add("app.aggregation_window: must be >= update_interval")
	}
	if c.Live.StaleAfter <= 0 {
		add("live.stale_after: must be > 0")
	}
	if c.API.Listen == "" {
		add("api.listen: required")
	}
	if len(c.Network.InternalCIDRs) == 0 {
		add("network.internal_cidrs: at least one CIDR required")
	}
	for _, s := range c.Network.InternalCIDRs {
		if _, err := netip.ParsePrefix(s); err != nil {
			add("network.internal_cidrs: %q: %v", s, err)
		}
	}
	o := c.Network.Origin
	if o.Latitude < -90 || o.Latitude > 90 || o.Longitude < -180 || o.Longitude > 180 {
		add("network.origin: latitude/longitude out of range")
	}
	switch o.Precision {
	case "country", "region", "city", "custom":
	default:
		add("network.origin.precision: must be country|region|city|custom, got %q", o.Precision)
	}
	if _, err := netip.ParseAddrPort(normalizeListen(c.Collector.Listen)); err != nil {
		add("collector.listen: %q: %v", c.Collector.Listen, err)
	}
	if c.Collector.Protocol != "sflow-v5" {
		add("collector.protocol: only sflow-v5 is supported, got %q", c.Collector.Protocol)
	}
	if c.Collector.Workers < 1 || c.Collector.Workers > 64 {
		add("collector.workers: must be in [1, 64]")
	}
	if c.Collector.QueueSize < 1 {
		add("collector.queue_size: must be > 0")
	}
	if c.Collector.SummaryInterval <= 0 {
		add("collector.summary_interval: must be > 0")
	}
	if c.Collector.DebugFlowsPerSecond < 0 {
		add("collector.debug_flows_per_second: must be >= 0")
	}
	for _, s := range c.Collector.AllowedSources {
		if _, err := ParseSource(s); err != nil {
			add("collector.allowed_sources: %q: %v", s, err)
		}
	}
	if !mock.IsScenarioName(c.Mock.Scenario) {
		add("mock.scenario: unknown %q (valid: %s)", c.Mock.Scenario, strings.Join(mock.ScenarioNames, ", "))
	}
	if c.Mock.Speed <= 0 || c.Mock.Speed > 100 {
		add("mock.speed: must be in (0, 100]")
	}
	if c.Mock.Seed < 0 {
		add("mock.seed: must be >= 0")
	}
	if c.Visualization.Globe.MaxArcs <= 0 || c.Visualization.Home.MaxEdges <= 0 || c.Visualization.Home.MaxNodes <= 0 {
		add("visualization: max_arcs, max_nodes and max_edges must be > 0")
	}
	switch c.Visualization.Globe.DefaultGrouping {
	case "country", "city", "asn", "ip":
	default:
		add("visualization.globe.default_grouping: invalid %q", c.Visualization.Globe.DefaultGrouping)
	}
	switch c.History.Backend {
	case "none":
	case "memory":
		if _, err := history.ParseRetention(c.History.MemoryRetention); err != nil {
			add("history.memory_retention: %v", err)
		}
	case "clickhouse":
		for name, v := range map[string]string{
			"raw_flow_retention": c.History.RawFlowRetention, "aggregate_1m_retention": c.History.Aggregate1mRetention,
			"aggregate_1h_retention": c.History.Aggregate1hRetention,
		} {
			if _, err := history.ParseRetention(v); err != nil {
				add("history.%s: %v", name, err)
			}
		}
		if !strings.HasPrefix(c.ClickHouse.DSN, "http://") && !strings.HasPrefix(c.ClickHouse.DSN, "https://") {
			add("clickhouse.dsn: history.backend clickhouse needs an HTTP URL like http://localhost:8123/ntv, got %q", c.ClickHouse.DSN)
		}
	default:
		add("history.backend: must be none, memory or clickhouse, got %q", c.History.Backend)
	}
	return errors.Join(errs...)
}

// ParseSource accepts a CIDR or a single address.
func ParseSource(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// normalizeListen turns ":6343" into "0.0.0.0:6343" for validation.
func normalizeListen(s string) string {
	if strings.HasPrefix(s, ":") {
		return "0.0.0.0" + s
	}
	return s
}
