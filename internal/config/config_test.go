package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaultsAreValid(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestExampleConfigLoads(t *testing.T) {
	c, err := Load("../../configs/config.example.yaml", env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.App.Mode != "mock" || len(c.Network.InternalCIDRs) != 4 || c.API.Listen == "" {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestPrecedenceYamlThenEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("mock:\n  scenario: heavy-download\n  seed: 7\napi:\n  listen: \":9000\"\n"), 0o600)
	c, err := Load(p, env(map[string]string{"MOCK_SEED": "99", "API_CORS_ALLOWED_ORIGINS": "http://a, http://b"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mock.Scenario != "heavy-download" || c.Mock.Seed != 99 || c.API.Listen != ":9000" {
		t.Fatalf("precedence wrong: %+v", c.Mock)
	}
	if strings.Join(c.API.CORSAllowedOrigins, "|") != "http://a|http://b" {
		t.Fatalf("cors: %v", c.API.CORSAllowedOrigins)
	}
	if c.App.UpdateInterval.Seconds() != 1 {
		t.Fatal("defaults should survive partial YAML")
	}
}

func TestValidationErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	os.WriteFile(p, []byte("network:\n  internal_cidrs: [\"10.0.0.0/33\"]\n  origin: {latitude: 95, longitude: 0, precision: street}\nmock:\n  scenario: nope\n"), 0o600)
	_, err := Load(p, env(nil))
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"internal_cidrs", "out of range", "precision", "mock.scenario"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestUnknownKeysRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "typo.yaml")
	os.WriteFile(p, []byte("app:\n  mdoe: live\n"), 0o600)
	if _, err := Load(p, env(nil)); err == nil {
		t.Fatal("typo in config key should be an error")
	}
}

func TestLiveMode(t *testing.T) {
	c, err := Load("", env(map[string]string{"APP_MODE": "live", "INVENTORY_FILE": "x.yaml"}))
	if err != nil || c.App.Mode != "live" || c.InventoryFile != "x.yaml" || c.Live.StaleAfter.Seconds() != 15 {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := Load("", env(map[string]string{"APP_MODE": "replay"})); err == nil {
		t.Fatal("unknown mode must fail")
	}
}

func TestCollectorConfig(t *testing.T) {
	c, err := Load("", env(map[string]string{"COLLECTOR_ALLOWED_SOURCES": "10.0.0.1, 192.0.2.0/24", "COLLECTOR_LISTEN": ":16343"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Collector.AllowedSources) != 2 || c.Collector.Listen != ":16343" || c.Collector.Workers != 2 {
		t.Fatalf("%+v", c.Collector)
	}
	p, _ := ParseSource("10.0.0.1")
	if p.String() != "10.0.0.1/32" {
		t.Fatal(p)
	}
	_, err = Load("", env(map[string]string{"COLLECTOR_ALLOWED_SOURCES": "nope", "COLLECTOR_LISTEN": "bad"}))
	if err == nil || !strings.Contains(err.Error(), "allowed_sources") || !strings.Contains(err.Error(), "collector.listen") {
		t.Fatalf("got %v", err)
	}
}
