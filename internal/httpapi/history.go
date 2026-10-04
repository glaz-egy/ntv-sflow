package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	c "network-traffic-visualizer/internal/contract"
	"network-traffic-visualizer/internal/history"
	"network-traffic-visualizer/internal/projection"
)

// timestamp parses an optional RFC3339 query parameter.
func (p *params) timestamp(name string) time.Time {
	v := p.str(name)
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		p.fail("%s must be an RFC3339 timestamp", name)
		return time.Time{}
	}
	return t.UTC()
}

// window returns the live window, or a historical one for start/end
// (D-059). It writes the error response itself and then reports false.
func (s *Server) window(w http.ResponseWriter, r *http.Request, p *params) (*projection.Frame, *projection.Inventory, bool) {
	start, end := p.timestamp("start"), p.timestamp("end")
	if p.err == nil && start.IsZero() != end.IsZero() {
		p.fail("start and end must be given together")
	}
	if p.err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILTER", p.err.Error())
		return nil, nil, false
	}
	if start.IsZero() {
		f, inv := s.src.Snapshot()
		return f, inv, true
	}
	if s.opts.History == nil {
		writeError(w, http.StatusNotImplemented, "HISTORY_UNAVAILABLE", "history is not enabled (history.backend)")
		return nil, nil, false
	}
	snap, err := s.opts.History.Snapshot(r.Context(), start, end)
	if err != nil {
		s.historyError(w, err)
		return nil, nil, false
	}
	return snap.Frame, snap.Inv, true
}

func (s *Server) historyError(w http.ResponseWriter, err error) {
	var re *history.RangeError
	if errors.As(err, &re) {
		writeError(w, http.StatusBadRequest, "INVALID_RANGE", re.Error())
		return
	}
	s.opts.Logger.Error("history query failed", "error", err.Error())
	writeError(w, http.StatusServiceUnavailable, "HISTORY_ERROR", "history store unavailable")
}

// statusResponse adds history availability to the source status.
func (s *Server) statusResponse(ctx context.Context) c.StatusResponse {
	st := s.src.Status()
	if s.opts.History == nil {
		return st
	}
	info, err := s.opts.History.Info(ctx)
	if err != nil {
		s.opts.Logger.Warn("history coverage unavailable", "error", err.Error())
	}
	hs := &c.HistoryStatus{Backend: info.Kind, Tiers: []c.HistoryTier{}}
	if info.Earliest != nil {
		hs.Earliest, hs.Latest = c.Ptr(c.TS(*info.Earliest)), c.Ptr(c.TS(*info.Latest))
	}
	for _, t := range info.Tiers {
		hs.Tiers = append(hs.Tiers, c.HistoryTier{StepSeconds: int(t.Step / time.Second), RetentionSeconds: int64(t.Retention / time.Second)})
	}
	st.History = hs
	return st
}

func (s *Server) timeline(w http.ResponseWriter, r *http.Request) {
	p := &params{q: r.URL.Query()}
	start, end := p.timestamp("start"), p.timestamp("end")
	maxPoints := p.int("max_points", 2, history.MaxTimelinePoints)
	if p.err == nil && (start.IsZero() || end.IsZero()) {
		p.fail("start and end are required")
	}
	if p.err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILTER", p.err.Error())
		return
	}
	if s.opts.History == nil {
		writeError(w, http.StatusNotImplemented, "HISTORY_UNAVAILABLE", "history is not enabled (history.backend)")
		return
	}
	tl, err := s.opts.History.Timeline(r.Context(), start, end, maxPoints)
	if err != nil {
		s.historyError(w, err)
		return
	}
	step := int(tl.Step / time.Second)
	counter := func(bps float64, covered float64) *c.Measurement {
		return &c.Measurement{Value: bps, Unit: "bps", MeasurementKind: c.KindCounter, IntervalSeconds: c.Ptr(int(covered + 0.5))}
	}
	resp := c.HistoryTimelineResponse{
		Window: c.TimeWindow{Start: c.TS(tl.Start), End: c.TS(tl.End)}, StepSeconds: step,
		Points: make([]c.HistoryTimelinePoint, 0, len(tl.Points)),
	}
	for _, pt := range tl.Points {
		out := c.HistoryTimelinePoint{Start: c.TS(pt.Start), HasData: pt.Data}
		secs := max(1, int(pt.Seconds+0.5))
		sampled := func(bps float64, n int) *c.Measurement {
			return &c.Measurement{Value: bps, Unit: "bps", MeasurementKind: c.KindSampledEstimate, WindowSeconds: c.Ptr(secs), SampleCount: c.Ptr(n)}
		}
		if pt.Data {
			out.Inbound = sampled(pt.Inbound, pt.InboundSamples)
			out.Outbound = sampled(pt.Outbound, pt.OutboundSamples)
			out.Internal = sampled(pt.Internal, pt.InternalSamples)
		}
		if pt.WanDownload != nil {
			out.WanDownload, out.WanUpload = counter(*pt.WanDownload, pt.WanCoveredSeconds), counter(*pt.WanUpload, pt.WanCoveredSeconds)
		}
		resp.Points = append(resp.Points, out)
	}
	writeJSON(w, http.StatusOK, resp)
}

// Flow-search cursors are opaque to clients: "o<offset>", base64url.
func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o" + strconv.Itoa(offset)))
}

func (p *params) cursor(name string) int {
	v := p.str(name)
	if v == "" {
		return 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(v)
	n, err2 := strconv.Atoi(strings.TrimPrefix(string(raw), "o"))
	if err != nil || err2 != nil || !strings.HasPrefix(string(raw), "o") || n < 0 {
		p.fail("%s is not a valid cursor", name)
		return 0
	}
	return n
}
