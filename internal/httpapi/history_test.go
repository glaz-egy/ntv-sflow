package httpapi

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"network-traffic-visualizer/internal/history"
	"network-traffic-visualizer/internal/mock"
	"network-traffic-visualizer/internal/realtime"
)

type directSink struct{ store history.Store }

func (d directSink) RecordFlows(rows []history.FlowRow)       { _ = d.store.WriteFlows(nil, rows) }
func (d directSink) RecordCounters(rows []history.CounterRow) { _ = d.store.WriteCounters(nil, rows) }

var histEpoch = time.Date(2026, 10, 3, 14, 45, 0, 0, time.UTC)

// historyServer runs the mock with a memory history for 90 sim seconds.
func historyServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := history.NewMemory(history.MemoryOptions{Retention: 24 * time.Hour, Now: func() time.Time { return histEpoch.Add(time.Hour) }})
	b, err := mock.NewBackend(mock.Options{Seed: 42, Scenario: "default", Speed: 1, Epoch: histEpoch, History: directSink{store}})
	if err != nil {
		t.Fatal(err)
	}
	for tick := mock.StartTick + 1; tick <= 90; tick++ {
		b.Advance(histEpoch.Add(time.Duration(tick) * time.Second))
	}
	svc := history.NewService(store, b, b.Now)
	ts := httptest.NewServer(New(b, realtime.NewHub(10), Options{History: svc}))
	t.Cleanup(ts.Close)
	return ts
}

func at(sec int) string {
	return url.QueryEscape(histEpoch.Add(time.Duration(sec) * time.Second).Format(time.RFC3339))
}

func TestHistoryEndpointsMatchOpenAPI(t *testing.T) {
	doc := loadSpec(t)
	ts := historyServer(t)
	rng := "start=" + at(40) + "&end=" + at(70)
	cases := []struct{ specPath, url string }{
		{"/status", "/api/v1/status"},
		{"/globe", "/api/v1/globe?grouping=asn&" + rng},
		{"/globe/destinations/{key}", "/api/v1/globe/destinations/" + url.PathEscape("asn:13335") + "?" + rng},
		{"/home/traffic", "/api/v1/home/traffic?" + rng},
		{"/home/topology", "/api/v1/home/topology?" + rng},
		{"/devices", "/api/v1/devices?" + rng},
		{"/devices/{id}", "/api/v1/devices/dev_pc01?" + rng},
		{"/flows", "/api/v1/flows?limit=3&" + rng},
		{"/history/timeline", "/api/v1/history/timeline?max_points=10&start=" + at(-20) + "&end=" + at(80)},
	}
	for _, tc := range cases {
		status, body, _ := get(t, ts, tc.url)
		if status != 200 {
			t.Errorf("%s: status %d: %v", tc.url, status, body)
			continue
		}
		schema := doc.Paths.Find(tc.specPath).Get.Responses.Status(200).Value.Content["application/json"].Schema.Value
		if err := schema.VisitJSON(body); err != nil {
			t.Errorf("%s does not match schema: %v", tc.url, err)
		}
	}

	_, st, _ := get(t, ts, "/api/v1/status")
	h := st.(map[string]any)["history"].(map[string]any)
	if h["backend"] != "memory" || h["earliest"] != "2026-10-03T14:45:00.000Z" || h["latest"] != "2026-10-03T14:46:30.000Z" {
		t.Errorf("history status: %v", h)
	}

	_, g, _ := get(t, ts, "/api/v1/globe?grouping=asn&"+rng)
	w := g.(map[string]any)["window"].(map[string]any)
	if w["start"] != "2026-10-03T14:45:40.000Z" || w["end"] != "2026-10-03T14:46:10.000Z" {
		t.Errorf("historical window: %v", w)
	}
	if n := len(g.(map[string]any)["destinations"].([]any)); n == 0 {
		t.Error("historical globe is empty")
	}

	_, tl, _ := get(t, ts, "/api/v1/history/timeline?max_points=10&start="+at(-20)+"&end="+at(80))
	tlm := tl.(map[string]any)
	pts := tlm["points"].([]any)
	if tlm["step_seconds"].(float64) != 10 || len(pts) != 10 {
		t.Fatalf("timeline: step %v, %d points", tlm["step_seconds"], len(pts))
	}
	if first := pts[0].(map[string]any); first["has_data"] != false || first["inbound"] != nil {
		t.Errorf("bucket before the recorded range must be empty: %v", first)
	}
	if p := pts[2].(map[string]any); p["has_data"] != true || p["outbound"] == nil || p["wan_download"] != nil {
		t.Errorf("[0s,10s) has samples but no counter poll yet: %v", p)
	}
	if p := pts[6].(map[string]any); p["wan_download"] == nil || p["wan_download"].(map[string]any)["measurement_kind"] != "counter" {
		t.Errorf("[40s,50s) must carry boundary counters: %v", p)
	}
	// "Last hour" with a client clock ahead of the server is clamped to now.
	if status, body, _ := get(t, ts, "/api/v1/history/timeline?start="+at(30)+"&end="+at(4000)); status != 200 {
		t.Errorf("future end must be clamped: %d %v", status, body)
	}
}

func TestFlowCursorPagesThroughResults(t *testing.T) {
	ts := historyServer(t)
	rng := "start=" + at(40) + "&end=" + at(70)
	_, all, _ := get(t, ts, "/api/v1/flows?limit=1000&"+rng)
	total := len(all.(map[string]any)["flows"].([]any))
	seen, cursor, pages := 0, "", 0
	for {
		u := "/api/v1/flows?limit=7&" + rng
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		status, body, _ := get(t, ts, u)
		if status != 200 {
			t.Fatalf("%s: %d %v", u, status, body)
		}
		m := body.(map[string]any)
		seen += len(m["flows"].([]any))
		pages++
		next, _ := m["next_cursor"].(string)
		if next == "" {
			if m["truncated_count"].(float64) != 0 {
				t.Errorf("last page must not be truncated: %v", m["truncated_count"])
			}
			break
		}
		cursor = next
		if pages > 100 {
			t.Fatal("cursor does not terminate")
		}
	}
	if seen != total || total <= 7 {
		t.Errorf("paged %d of %d flows in %d pages", seen, total, pages)
	}
}

func TestHistoryErrors(t *testing.T) {
	ts := historyServer(t)
	live, _, _ := newTestServer(t, "default") // no history configured
	cases := []struct {
		ts     *httptest.Server
		url    string
		status int
		code   string
	}{
		{ts, "/api/v1/globe?grouping=asn&start=" + at(40), 400, "INVALID_FILTER"},
		{ts, "/api/v1/globe?grouping=asn&start=yesterday&end=" + at(40), 400, "INVALID_FILTER"},
		{ts, "/api/v1/globe?grouping=asn&start=" + at(70) + "&end=" + at(40), 400, "INVALID_RANGE"},
		{ts, "/api/v1/globe?grouping=asn&start=" + at(40) + "&end=" + at(5000), 400, "INVALID_RANGE"},
		{ts, "/api/v1/flows?cursor=bogus", 400, "INVALID_FILTER"},
		{ts, "/api/v1/history/timeline?start=" + at(0), 400, "INVALID_FILTER"},
		{live, "/api/v1/globe?grouping=asn&start=" + at(40) + "&end=" + at(70), 501, "HISTORY_UNAVAILABLE"},
		{live, "/api/v1/history/timeline?start=" + at(0) + "&end=" + at(70), 501, "HISTORY_UNAVAILABLE"},
	}
	for _, c := range cases {
		status, body, _ := get(t, c.ts, c.url)
		code := body.(map[string]any)["error"].(map[string]any)["code"]
		if status != c.status || code != c.code {
			t.Errorf("%s: got %d %v, want %d %s", c.url, status, code, c.status, c.code)
		}
	}
	_, st, _ := get(t, live, "/api/v1/status")
	if st.(map[string]any)["history"] != nil {
		t.Error("status.history must be null without history")
	}
}
