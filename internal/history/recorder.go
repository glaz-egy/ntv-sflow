package history

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"time"
)

// Sink receives completed seconds from a live or mock source. It must not
// block: the collector path never waits for storage (D-047).
type Sink interface {
	RecordFlows(rows []FlowRow)
	RecordCounters(rows []CounterRow)
}

type RecorderOptions struct {
	// QueueSize bounds batches waiting to be written (default 256).
	QueueSize int
	// FlushInterval batches writes (default 2s).
	FlushInterval time.Duration
	// MaxPendingRows bounds rows kept for retry after a failed write
	// (default 200,000); older rows beyond it are dropped and counted.
	MaxPendingRows int
	Logger         *slog.Logger
}

type batch struct {
	flows    []FlowRow
	counters []CounterRow
}

// Recorder writes to a Store asynchronously with bounded memory.
type Recorder struct {
	store Store
	opts  RecorderOptions
	ch    chan batch

	queuedDropped  atomic.Int64 // rows dropped because the queue was full
	pendingDropped atomic.Int64 // rows dropped after failed writes
	writtenFlows   atomic.Int64
	writtenCounter atomic.Int64
	writeErrors    atomic.Int64
	lastErrorLog   time.Time
}

func NewRecorder(store Store, opts RecorderOptions) *Recorder {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 256
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 2 * time.Second
	}
	if opts.MaxPendingRows <= 0 {
		opts.MaxPendingRows = 200_000
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Recorder{store: store, opts: opts, ch: make(chan batch, opts.QueueSize)}
}

func (r *Recorder) RecordFlows(rows []FlowRow) {
	if len(rows) > 0 {
		r.enqueue(batch{flows: rows}, len(rows))
	}
}

func (r *Recorder) RecordCounters(rows []CounterRow) {
	if len(rows) > 0 {
		r.enqueue(batch{counters: rows}, len(rows))
	}
}

func (r *Recorder) enqueue(b batch, n int) {
	select {
	case r.ch <- b:
	default:
		r.queuedDropped.Add(int64(n))
	}
}

// Run writes batches until ctx is done, then flushes what is queued.
func (r *Recorder) Run(ctx context.Context) {
	t := time.NewTicker(r.opts.FlushInterval)
	defer t.Stop()
	var pend batch
	for {
		select {
		case b := <-r.ch:
			pend.flows = append(pend.flows, b.flows...)
			pend.counters = append(pend.counters, b.counters...)
		case <-t.C:
			pend = r.flush(ctx, pend)
		case <-ctx.Done():
		drain:
			for {
				select {
				case b := <-r.ch:
					pend.flows = append(pend.flows, b.flows...)
					pend.counters = append(pend.counters, b.counters...)
				default:
					break drain
				}
			}
			// Best effort on shutdown, bounded so stopping never hangs.
			final, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			r.flush(final, pend)
			cancel()
			return
		}
	}
}

// flush writes pending rows; failed rows stay pending (bounded).
func (r *Recorder) flush(ctx context.Context, p batch) batch {
	if len(p.flows) > 0 {
		if err := r.store.WriteFlows(ctx, p.flows); err != nil {
			r.fail("flows", err)
		} else {
			r.writtenFlows.Add(int64(len(p.flows)))
			p.flows = nil
		}
	}
	if len(p.counters) > 0 {
		if err := r.store.WriteCounters(ctx, p.counters); err != nil {
			r.fail("counters", err)
		} else {
			r.writtenCounter.Add(int64(len(p.counters)))
			p.counters = nil
		}
	}
	if over := len(p.flows) - r.opts.MaxPendingRows; over > 0 {
		r.pendingDropped.Add(int64(over))
		p.flows = append([]FlowRow(nil), p.flows[over:]...)
	}
	if over := len(p.counters) - r.opts.MaxPendingRows; over > 0 {
		r.pendingDropped.Add(int64(over))
		p.counters = append([]CounterRow(nil), p.counters[over:]...)
	}
	return p
}

func (r *Recorder) fail(what string, err error) {
	r.writeErrors.Add(1)
	if time.Since(r.lastErrorLog) > time.Minute {
		r.lastErrorLog = time.Now()
		r.opts.Logger.Error("history write failed; rows kept for retry", "rows", what, "error", err.Error())
	}
}

// WritePrometheus exposes recorder metrics (docs/OBSERVABILITY.md).
func (r *Recorder) WritePrometheus(w io.Writer) {
	fmt.Fprintf(w, "# HELP ntv_history_rows_written_total Rows written to the history store.\n# TYPE ntv_history_rows_written_total counter\n")
	fmt.Fprintf(w, "ntv_history_rows_written_total{kind=\"flow\"} %d\nntv_history_rows_written_total{kind=\"counter\"} %d\n", r.writtenFlows.Load(), r.writtenCounter.Load())
	fmt.Fprintf(w, "# HELP ntv_history_rows_dropped_total Rows not persisted (queue full or retry buffer exceeded).\n# TYPE ntv_history_rows_dropped_total counter\n")
	fmt.Fprintf(w, "ntv_history_rows_dropped_total{reason=\"queue_full\"} %d\nntv_history_rows_dropped_total{reason=\"retry_overflow\"} %d\n", r.queuedDropped.Load(), r.pendingDropped.Load())
	fmt.Fprintf(w, "# HELP ntv_history_write_errors_total Failed history writes.\n# TYPE ntv_history_write_errors_total counter\nntv_history_write_errors_total %d\n", r.writeErrors.Load())
}
