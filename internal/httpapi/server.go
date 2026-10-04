// Package httpapi serves the REST + WebSocket API (api/openapi.yaml).
// Handlers validate input, ask a Source for the current window and map it
// through the projection package. They never see collector/sFlow types.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"

	"network-traffic-visualizer/internal/aggregation"
	c "network-traffic-visualizer/internal/contract"
	"network-traffic-visualizer/internal/history"
	"network-traffic-visualizer/internal/projection"
	"network-traffic-visualizer/internal/realtime"
)

// Source provides the current aggregate window (mock now, live later).
type Source interface {
	Snapshot() (*projection.Frame, *projection.Inventory)
	Status() c.StatusResponse
}

type Options struct {
	CORSAllowedOrigins []string
	HeartbeatInterval  time.Duration
	Logger             *slog.Logger
	// Metrics, when set, serves Prometheus text at GET /metrics.
	Metrics func(io.Writer)
	// History, when set, serves historical windows (start/end) and the
	// timeline (D-059).
	History *history.Service
}

type Server struct {
	src  Source
	hub  *realtime.Hub
	opts Options
	mux  *http.ServeMux
}

func New(src Source, hub *realtime.Hub, opts Options) *Server {
	if opts.HeartbeatInterval == 0 {
		opts.HeartbeatInterval = 15 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	s := &Server{src: src, hub: hub, opts: opts, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	s.mux.HandleFunc("GET /api/v1/status", s.status)
	s.mux.HandleFunc("GET /api/v1/globe", s.globe)
	s.mux.HandleFunc("GET /api/v1/globe/destinations/{key}", s.destination)
	s.mux.HandleFunc("GET /api/v1/home/traffic", s.homeTraffic)
	s.mux.HandleFunc("GET /api/v1/home/topology", s.topology)
	s.mux.HandleFunc("GET /api/v1/devices", s.devices)
	s.mux.HandleFunc("GET /api/v1/devices/{id}", s.device)
	s.mux.HandleFunc("GET /api/v1/flows", s.flows)
	s.mux.HandleFunc("GET /api/v1/history/timeline", s.timeline)
	s.mux.HandleFunc("GET /api/v1/ws", s.websocket)
	if opts.Metrics != nil {
		s.mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			opts.Metrics(w)
		})
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && s.originAllowed(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) originAllowed(origin string) bool {
	for _, o := range s.opts.CORSAllowedOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, c.ApiError{Error: c.ApiErrorBody{Code: code, Message: msg}})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.statusResponse(r.Context()))
}

func (s *Server) globe(w http.ResponseWriter, r *http.Request) {
	p := &params{q: r.URL.Query()}
	q := projection.GlobeQuery{
		Grouping:     p.enum("grouping", true, groupings...),
		SourceNodeID: p.str("source_node_id"),
		Direction:    p.enum("direction", false, "inbound", "outbound", "both"),
		Protocol:     p.enum("protocol", false, protocols...),
		MinBps:       p.float("min_bps", 0),
		Limit:        p.int("limit", 1, 1000),
	}
	if p.err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILTER", p.err.Error())
		return
	}
	f, inv, ok := s.window(w, r, p)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, inv.Globe(f, q))
}

func (s *Server) destination(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if _, _, ok := aggregation.ParseDestinationKey(key); !ok {
		writeError(w, http.StatusBadRequest, "INVALID_KEY", "destination key must be <grouping>:<value>")
		return
	}
	p := &params{q: r.URL.Query()}
	src, proto := p.str("source_node_id"), p.enum("protocol", false, protocols...)
	if p.err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILTER", p.err.Error())
		return
	}
	f, inv, ok := s.window(w, r, p)
	if !ok {
		return
	}
	d := inv.Destination(f, key, src, proto)
	if d == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no matching traffic for this destination in the current window")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) homeTraffic(w http.ResponseWriter, r *http.Request) {
	p := &params{q: r.URL.Query()}
	q := projection.HomeQuery{
		FocusNodeID:     p.str("focus_node_id"),
		DestinationKey:  p.str("destination_key"),
		VlanID:          p.intPtr("vlan_id", 0, 4095),
		DeviceTypes:     p.list("device_types", deviceTypes...),
		Protocol:        p.enum("protocol", false, protocols...),
		MinBps:          p.float("min_bps", 0),
		Limit:           p.int("limit", 1, 2000),
		IncludeInactive: p.boolean("include_inactive"),
		Scope:           p.enum("scope", false, "internal", "external", "both"),
	}
	if q.DestinationKey != "" {
		if _, _, ok := aggregation.ParseDestinationKey(q.DestinationKey); !ok {
			p.fail("destination_key must be <grouping>:<value>")
		}
	}
	if p.err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILTER", p.err.Error())
		return
	}
	f, inv, ok := s.window(w, r, p)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, inv.Home(f, q))
}

func (s *Server) topology(w http.ResponseWriter, r *http.Request) {
	f, inv, ok := s.window(w, r, &params{q: r.URL.Query()})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, inv.Topology(f))
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	p := &params{q: r.URL.Query()}
	q := projection.DeviceFilter{
		Type:   p.enum("type", false, deviceTypes...),
		Status: p.enum("status", false, "online", "stale", "offline", "unknown"),
		VlanID: p.intPtr("vlan", 0, 4095),
		Search: p.str("search"),
	}
	if p.err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILTER", p.err.Error())
		return
	}
	f, inv, ok := s.window(w, r, p)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, inv.Devices(f, q))
}

func (s *Server) device(w http.ResponseWriter, r *http.Request) {
	p := &params{q: r.URL.Query()}
	grouping := p.enum("grouping", false, groupings...)
	if grouping == "" {
		grouping = "asn"
	}
	if p.err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILTER", p.err.Error())
		return
	}
	f, inv, ok := s.window(w, r, p)
	if !ok {
		return
	}
	d := inv.Device(f, r.PathValue("id"), grouping)
	if d == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "unknown device")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) flows(w http.ResponseWriter, r *http.Request) {
	p := &params{q: r.URL.Query()}
	q := projection.FlowQuery{
		Source: p.str("source"), Destination: p.str("destination"),
		SourceNodeID: p.str("source_node_id"), DestNodeID: p.str("destination_node_id"),
		Protocol: p.enum("protocol", false, protocols...), Exporter: p.str("exporter"),
		Scope:   p.enum("internal_scope", false, "internal", "external", "transit"),
		SrcPort: p.intPtr("src_port", 0, 65535), DstPort: p.intPtr("dst_port", 0, 65535),
		Limit: p.int("limit", 1, 1000), Offset: p.cursor("cursor"),
	}
	if p.err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILTER", p.err.Error())
		return
	}
	f, inv, ok := s.window(w, r, p)
	if !ok {
		return
	}
	resp := inv.Flows(f, q)
	if resp.NextOffset != nil {
		resp.NextCursor = c.Ptr(encodeCursor(*resp.NextOffset))
	}
	writeJSON(w, http.StatusOK, resp)
}

// WindowUpdate builds the envelope broadcast once per aggregate window.
func (s *Server) WindowUpdate() c.ServerEnvelope {
	st := s.statusResponse(context.Background())
	end := st.ServerTime
	if st.LastAggregateAt != nil {
		end = *st.LastAggregateAt
	}
	return s.hub.Envelope("window_update", c.WindowUpdatePayload{WindowEnd: end, Status: st})
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	var patterns []string
	for _, o := range s.opts.CORSAllowedOrigins {
		if o == "*" {
			patterns = append(patterns, "*")
		} else if u, err := url.Parse(o); err == nil && u.Host != "" {
			patterns = append(patterns, u.Host)
		}
	}
	client, unsubscribe, ok := s.hub.Subscribe()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "TOO_MANY_CLIENTS", "WebSocket client limit reached")
		return
	}
	defer unsubscribe()
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: patterns})
	if err != nil {
		return // Accept already wrote the response
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Reader: accept subscribe messages; window_update is global for now.
	go func() {
		defer cancel()
		for {
			var msg c.SubscribeMessage
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if json.Unmarshal(data, &msg) != nil || msg.Type != "subscribe" {
				env := s.hub.Envelope("error", c.ApiErrorBody{Code: "INVALID_MESSAGE", Message: "expected {\"type\":\"subscribe\"}"})
				b, _ := json.Marshal(env)
				_ = conn.Write(ctx, websocket.MessageText, b)
			}
		}
	}()

	send := func(env c.ServerEnvelope) error {
		b, err := json.Marshal(env)
		if err != nil {
			return err
		}
		wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
		defer wcancel()
		return conn.Write(wctx, websocket.MessageText, b)
	}
	if send(s.WindowUpdate()) != nil {
		return
	}
	heartbeat := time.NewTicker(s.opts.HeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return
		case msg := <-client.Send:
			wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Write(wctx, websocket.MessageText, msg)
			wcancel()
			if err != nil {
				return
			}
		case <-heartbeat.C:
			if send(s.hub.Envelope("heartbeat", map[string]any{})) != nil {
				return
			}
		}
	}
}
