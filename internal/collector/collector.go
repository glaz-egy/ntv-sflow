// Package collector receives sFlow v5 over UDP and turns it into normalized
// domain observations. Pipeline (docs/SFLOW.md §3):
//
//	UDP receive → bounded queue → decode workers → normalize → Sink
//
// The receive loop only copies bytes; decoding and everything heavier runs
// in workers. Enrichment (GeoIP, devices) is not done here.
package collector

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"network-traffic-visualizer/internal/counters"
	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/sflow"
)

// Batch is everything normalized from one datagram.
type Batch struct {
	ReceivedAt time.Time
	Source     netip.AddrPort
	Flows      []flow.Sample
	Counters   []counters.Observation
}

// Sink consumes batches. Implementations must be safe for concurrent use
// and must not block for long (they run on decode workers).
type Sink interface {
	Publish(Batch)
}

type Options struct {
	Listen    string
	Workers   int
	QueueSize int
	// AllowedSources restricts accepted datagram source addresses (empty = all).
	AllowedSources []netip.Prefix
	ReadBuffer     int
	Logger         *slog.Logger
	Now            func() time.Time
}

type job struct {
	data []byte
	src  netip.AddrPort
	at   time.Time
}

// agentState tracks datagram sequence numbers since the agent's last
// restart. Loss is estimated as (highest − first + 1) − received, which is
// insensitive to the reordering that parallel workers introduce.
type agentState struct {
	first, highest uint32
	received       uint64
	uptimeMs       uint32
	lostBefore     int64 // loss accumulated before the last restart
}

func (a agentState) lost() int64 {
	missing := int64(a.highest) - int64(a.first) + 1 - int64(a.received)
	if missing < 0 {
		missing = 0
	}
	return a.lostBefore + missing
}

type Collector struct {
	opts    Options
	sink    Sink
	metrics *Metrics
	queue   chan job
	logs    *rateLimitedLog

	mu     sync.Mutex
	conn   *net.UDPConn
	agents map[string]agentState
	drops  map[string]uint32
	ready  chan struct{}
}

func New(opts Options, sink Sink) *Collector {
	if opts.Workers <= 0 {
		opts.Workers = 2
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = 4096
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Collector{
		opts: opts, sink: sink, metrics: newMetrics(opts.QueueSize),
		queue: make(chan job, opts.QueueSize), agents: map[string]agentState{}, drops: map[string]uint32{},
		logs: newRateLimitedLog(opts.Logger, 10*time.Second), ready: make(chan struct{}),
	}
}

func (c *Collector) Metrics() *Metrics { return c.metrics }

// Addr waits until the socket is bound and returns its address.
func (c *Collector) Addr(ctx context.Context) (net.Addr, error) {
	select {
	case <-c.ready:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.conn.LocalAddr(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Run listens until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp", c.opts.Listen)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	if c.opts.ReadBuffer > 0 {
		_ = conn.SetReadBuffer(c.opts.ReadBuffer)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	close(c.ready)

	var wg sync.WaitGroup
	for i := 0; i < c.opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range c.queue {
				c.metrics.QueueDepth.Add(-1)
				c.process(j)
			}
		}()
	}
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 65535)
	for {
		n, src, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			c.logs.warn("udp_read", "UDP read failed", "error", err.Error())
			continue
		}
		c.metrics.DatagramsReceived.Add(1)
		if !c.allowed(src.Addr()) {
			c.metrics.DatagramsRejected.Add(1)
			continue
		}
		j := job{data: append([]byte(nil), buf[:n]...), src: src, at: c.opts.Now()}
		select {
		case c.queue <- j:
			c.metrics.QueueDepth.Add(1)
		default:
			// Backpressure: drop rather than block the socket (D-047).
			c.metrics.DatagramsDropped.Add(1)
			c.logs.warn("queue_full", "ingestion queue full; dropping datagrams")
		}
	}
	close(c.queue)
	wg.Wait()
	return nil
}

func (c *Collector) allowed(a netip.Addr) bool {
	if len(c.opts.AllowedSources) == 0 {
		return true
	}
	a = a.Unmap()
	for _, p := range c.opts.AllowedSources {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// HandleDatagram decodes and publishes one datagram synchronously (tests, replay).
func (c *Collector) HandleDatagram(data []byte, src netip.AddrPort, at time.Time) {
	c.metrics.DatagramsReceived.Add(1)
	c.process(job{data: data, src: src, at: at})
}

func (c *Collector) process(j job) {
	d, err := sflow.Decode(j.data)
	if err != nil {
		kind := sflow.ErrorKind(err)
		c.metrics.decodeError(kind)
		c.logs.warn("decode_"+kind, "invalid sFlow datagram", "kind", kind, "source", j.src.Addr().String())
		return
	}
	m := c.metrics
	m.UnsupportedSamples.Add(int64(d.Stats.UnknownSamples))
	m.UnsupportedRecords.Add(int64(d.Stats.UnknownRecords))
	m.MalformedSamples.Add(int64(d.Stats.MalformedSamples))
	m.MalformedRecords.Add(int64(d.Stats.MalformedRecords))
	c.trackSequence(d)

	flows, ctrs, st := Normalize(d, j.at)
	m.NonIPSamples.Add(int64(st.NonIPSamples))
	m.InvalidSamplingRate.Add(int64(st.InvalidSamplingRate))
	m.FlowSamples.Add(int64(len(flows)))
	m.CounterSamples.Add(int64(len(ctrs)))
	if len(flows) > 0 || len(ctrs) > 0 {
		c.sink.Publish(Batch{ReceivedAt: j.at, Source: j.src, Flows: flows, Counters: ctrs})
	}
	m.ProcessingLagNanos.Store(int64(c.opts.Now().Sub(j.at)))
}

// trackSequence estimates lost datagrams and detects agent restarts
// (uptime going backwards, or a large sequence jump either way).
func (c *Collector) trackSequence(d *sflow.Datagram) {
	key := ExporterID(d.AgentAddress, d.SubAgentID)
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.agents[key]
	seq := d.SequenceNumber
	restarted := ok && ((d.UptimeMs < st.uptimeMs && st.uptimeMs-d.UptimeMs > 60_000) ||
		int64(seq) < int64(st.first)-1_000 || int64(seq) > int64(st.highest)+1_000_000)
	switch {
	case !ok:
		st = agentState{first: seq, highest: seq}
	case restarted:
		c.metrics.AgentRestarts.Add(1)
		st = agentState{first: seq, highest: seq, lostBefore: st.lost()}
	default:
		if seq > st.highest {
			st.highest = seq
		}
		if seq < st.first {
			st.first = seq
		}
	}
	st.received++
	if d.UptimeMs > st.uptimeMs || restarted || !ok {
		st.uptimeMs = d.UptimeMs
	}
	c.agents[key] = st
	var total int64
	for _, a := range c.agents {
		total += a.lost()
	}
	c.metrics.DatagramsLost.Store(total)
	// `drops` is a running total per data source: count only increases.
	for _, fs := range d.FlowSamples {
		src := key + "#" + strconv.FormatUint(uint64(fs.SourceIDType)<<32|uint64(fs.SourceIDIndex), 10)
		if last, seen := c.drops[src]; seen && fs.Drops > last {
			c.metrics.AgentDrops.Add(int64(fs.Drops - last))
		}
		c.drops[src] = fs.Drops
	}
}

// rateLimitedLog logs each key at most once per interval, with a count of
// suppressed occurrences (docs/SFLOW.md §12).
type rateLimitedLog struct {
	log      *slog.Logger
	interval time.Duration
	mu       sync.Mutex
	last     map[string]time.Time
	skipped  map[string]int
}

func newRateLimitedLog(l *slog.Logger, interval time.Duration) *rateLimitedLog {
	return &rateLimitedLog{log: l, interval: interval, last: map[string]time.Time{}, skipped: map[string]int{}}
}

func (r *rateLimitedLog) warn(key, msg string, args ...any) {
	r.mu.Lock()
	now := time.Now()
	if t, ok := r.last[key]; ok && now.Sub(t) < r.interval {
		r.skipped[key]++
		r.mu.Unlock()
		return
	}
	suppressed := r.skipped[key]
	r.last[key], r.skipped[key] = now, 0
	r.mu.Unlock()
	r.log.Warn(msg, append(args, "suppressed_since_last", suppressed)...)
}
