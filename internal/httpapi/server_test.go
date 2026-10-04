package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"io"
	"log/slog"
	"net/netip"

	"github.com/coder/websocket"
	"github.com/getkin/kin-openapi/openapi3"

	"network-traffic-visualizer/internal/collector"
	c "network-traffic-visualizer/internal/contract"
	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/inventory"
	"network-traffic-visualizer/internal/live"
	"network-traffic-visualizer/internal/mock"
	"network-traffic-visualizer/internal/realtime"
	"network-traffic-visualizer/internal/sflowgen"
)

func newTestServer(t *testing.T, scenario string) (*httptest.Server, *Server, *realtime.Hub) {
	t.Helper()
	b, err := mock.NewBackend(mock.Options{Seed: 42, Scenario: scenario, Speed: 1, Epoch: time.Date(2026, 10, 3, 14, 45, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	b.SetTick(mock.StartTick + 10)
	hub := realtime.NewHub(10)
	api := New(b, hub, Options{CORSAllowedOrigins: []string{"http://localhost:3000"}, HeartbeatInterval: time.Hour})
	ts := httptest.NewServer(api)
	t.Cleanup(ts.Close)
	return ts, api, hub
}

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml is invalid: %v", err)
	}
	return doc
}

func get(t *testing.T, ts *httptest.Server, path string) (int, any, http.Header) {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("%s: invalid JSON: %v", path, err)
	}
	return resp.StatusCode, body, resp.Header
}

// liveServer serves the live aggregator fed with sFlow from the generator
// (fed=false: before any datagram, status "disconnected").
func liveServer(t *testing.T, fed bool) *httptest.Server {
	t.Helper()
	epoch := time.Date(2026, 10, 3, 14, 45, 0, 0, time.UTC)
	inv, err := inventory.Load("../../configs/inventory.demo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	src, err := live.New(live.Options{
		Epoch: epoch, Inventory: inv, InternalCIDRs: mock.DefaultInternalCIDRs, Origin: mock.DefaultOrigin,
		Geo: enrichment.NewChainGeo(enrichment.NewOverrideGeo(inv.GeoOverrides), nil, 100),
		Now: func() time.Time { return epoch },
	})
	if err != nil {
		t.Fatal(err)
	}
	if fed {
		sc, _ := mock.BuildScenario("unknown-metadata", 42)
		engine, _ := mock.NewEngine(sc, 42)
		gen := sflowgen.New(engine)
		col := collector.New(collector.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, src)
		for sec := 30; sec <= 40; sec++ {
			src.Advance(epoch.Add(time.Duration(sec) * time.Second))
			for _, d := range gen.Second(sec, uint32(sec)) {
				col.HandleDatagram(d, netip.AddrPort{}, epoch.Add(time.Duration(sec)*time.Second))
			}
		}
	}
	src.Advance(epoch.Add(41 * time.Second))
	ts := httptest.NewServer(New(src, realtime.NewHub(10), Options{}))
	t.Cleanup(ts.Close)
	return ts
}

// Every endpoint response must satisfy the OpenAPI schema (contract test),
// for the mock backend and for live data from the sFlow collector.
func TestResponsesMatchOpenAPI(t *testing.T) {
	doc := loadSpec(t)
	servers := map[string]*httptest.Server{"live (no data yet)": liveServer(t, false), "live": liveServer(t, true)}
	for _, scenario := range []string{"default", "unknown-metadata", "stale-collector"} {
		ts, _, _ := newTestServer(t, scenario)
		servers[scenario] = ts
	}
	for scenario, ts := range servers {
		cases := []struct{ specPath, url string }{
			{"/status", "/api/v1/status"},
			{"/globe", "/api/v1/globe?grouping=asn"},
			{"/globe", "/api/v1/globe?grouping=ip&source_node_id=dev_pc01&direction=inbound&protocol=tcp&min_bps=1000&limit=5"},
			{"/globe", "/api/v1/globe?grouping=country"},
			{"/globe/destinations/{key}", "/api/v1/globe/destinations/" + url.PathEscape("asn:13335")},
			{"/home/traffic", "/api/v1/home/traffic"},
			{"/home/traffic", "/api/v1/home/traffic?destination_key=asn%3A13335&include_inactive=true&scope=both&device_types=pc,smartphone"},
			{"/home/topology", "/api/v1/home/topology"},
			{"/devices", "/api/v1/devices?search=pc"},
			{"/devices/{id}", "/api/v1/devices/dev_pc01?grouping=country"},
			{"/devices/{id}", "/api/v1/devices/" + url.PathEscape("ep:10.30.0.77")},
			{"/flows", "/api/v1/flows?protocol=tcp&limit=20"},
		}
		for _, tc := range cases {
			status, body, _ := get(t, ts, tc.url)
			if status == 404 && scenario == "live (no data yet)" {
				continue // destination / unresolved endpoint not seen yet
			}
			if status != 200 {
				t.Errorf("%s %s: status %d: %v", scenario, tc.url, status, body)
				continue
			}
			schema := doc.Paths.Find(tc.specPath).Get.Responses.Status(200).Value.Content["application/json"].Schema.Value
			if err := schema.VisitJSON(body); err != nil {
				t.Errorf("%s %s does not match schema: %v", scenario, tc.url, err)
			}
		}
	}
}

func TestInvalidParametersReturnApiError(t *testing.T) {
	doc := loadSpec(t)
	errSchema := doc.Components.Schemas["ApiError"].Value
	ts, _, _ := newTestServer(t, "default")
	for _, u := range []string{
		"/api/v1/globe",                         // grouping required
		"/api/v1/globe?grouping=planet",         // bad enum
		"/api/v1/globe?grouping=asn&min_bps=-1", // negative
		"/api/v1/globe?grouping=asn&limit=0",
		"/api/v1/home/traffic?vlan_id=abc",
		"/api/v1/home/traffic?device_types=pc,toaster",
		"/api/v1/home/traffic?destination_key=nonsense",
		"/api/v1/globe/destinations/planet:earth",
		"/api/v1/flows?src_port=70000",
	} {
		status, body, _ := get(t, ts, u)
		if status != 400 {
			t.Errorf("%s: want 400, got %d", u, status)
		}
		if err := errSchema.VisitJSON(body); err != nil {
			t.Errorf("%s: error body does not match ApiError: %v", u, err)
		}
	}
}

func TestNotFound(t *testing.T) {
	ts, _, _ := newTestServer(t, "default")
	for _, u := range []string{"/api/v1/devices/dev_nope", "/api/v1/globe/destinations/asn:1", "/api/v1/devices/ep:10.99.99.99"} {
		if status, _, _ := get(t, ts, u); status != 404 {
			t.Errorf("%s: want 404, got %d", u, status)
		}
	}
}

func TestCORS(t *testing.T) {
	ts, _, _ := newTestServer(t, "default")
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/status", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Error("allowed origin should get CORS header")
	}
	req.Header.Set("Origin", "http://evil.example")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("unknown origin must not get CORS header")
	}
}

// API and in-browser mock must agree: Globe sources == Home destination sources.
func TestCrossViewConsistencyOverHTTP(t *testing.T) {
	ts, _, _ := newTestServer(t, "default")
	var globe c.GlobeResponse
	_, raw, _ := get(t, ts, "/api/v1/globe?grouping=asn")
	b, _ := json.Marshal(raw)
	json.Unmarshal(b, &globe)
	for _, d := range globe.Destinations {
		var home c.HomeTrafficResponse
		_, raw, _ := get(t, ts, "/api/v1/home/traffic?destination_key="+url.QueryEscape(d.Key))
		b, _ := json.Marshal(raw)
		json.Unmarshal(b, &home)
		var detail c.GlobeDestinationDetail
		_, raw, _ = get(t, ts, "/api/v1/globe/destinations/"+url.PathEscape(d.Key))
		b, _ = json.Marshal(raw)
		json.Unmarshal(b, &detail)
		var srcs []string
		for _, s := range detail.TopSources {
			srcs = append(srcs, s.NodeID)
		}
		want := strings.Join(sortedCopy(srcs), ",")
		if got := strings.Join(home.DestinationContext.SourceNodeIDs, ","); got != want {
			t.Errorf("%s: home sources %s != globe sources %s", d.Key, got, want)
		}
	}
}

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestWebSocketWindowUpdates(t *testing.T) {
	doc := loadSpec(t)
	envSchema := doc.Components.Schemas["ServerEnvelope"].Value
	payloadSchema := doc.Components.Schemas["WindowUpdatePayload"].Value
	ts, api, hub := newTestServer(t, "default")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/v1/ws",
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:3000"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.Write(ctx, websocket.MessageText, []byte(`{"type":"subscribe","channels":["status"]}`))

	read := func() map[string]any {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var env map[string]any
		json.Unmarshal(data, &env)
		if err := envSchema.VisitJSON(env); err != nil {
			t.Fatalf("envelope schema: %v", err)
		}
		return env
	}
	first := read() // initial window_update on connect
	if first["type"] != "window_update" {
		t.Fatalf("want initial window_update, got %v", first["type"])
	}
	if err := payloadSchema.VisitJSON(first["payload"]); err != nil {
		t.Fatalf("payload schema: %v", err)
	}
	for hub.Clients() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	hub.Broadcast(api.WindowUpdate())
	second := read()
	if second["type"] != "window_update" || second["sequence"].(float64) <= first["sequence"].(float64) {
		t.Fatalf("broadcast not received in order: %v", second)
	}
}

func TestWebSocketRejectsForeignOrigin(t *testing.T) {
	ts, _, _ := newTestServer(t, "default")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/v1/ws",
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://evil.example"}}})
	if err == nil {
		t.Fatal("foreign origin should be rejected")
	}
}
