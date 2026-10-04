package history

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"network-traffic-visualizer/internal/flow"
)

// ClickHouse stores history in ClickHouse over its HTTP interface (port
// 8123; no driver dependency). Raw per-second rows are rolled up into
// 1-minute and 1-hour tables by materialized views; each table has its own
// TTL (history.* retention, D-059).
type ClickHouse struct {
	endpoint   *url.URL // scheme://host:port/ (no path)
	db         string
	user, pass string
	client     *http.Client
	tiers      []Tier
	log        *slog.Logger
}

type ClickHouseOptions struct {
	RawRetention, MinuteRetention, HourRetention time.Duration
	Timeout                                      time.Duration
	Logger                                       *slog.Logger
}

var tierTables = map[time.Duration]string{time.Second: "flow_seconds", time.Minute: "flow_minutes", time.Hour: "flow_hours"}

// OpenClickHouse connects, creates the database and applies migrations and
// retention. dsn: http(s)://[user[:password]@]host:8123/<database>.
func OpenClickHouse(ctx context.Context, dsn string, opts ClickHouseOptions) (*ClickHouse, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("clickhouse dsn: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("clickhouse dsn must be an HTTP URL like http://host:8123/ntv (got scheme %q)", u.Scheme)
	}
	db := strings.Trim(u.Path, "/")
	if db == "" {
		db = "ntv"
	}
	if !validIdent(db) {
		return nil, fmt.Errorf("clickhouse database name %q: use letters, digits and _", db)
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	c := &ClickHouse{
		endpoint: &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}, db: db,
		client: &http.Client{Timeout: opts.Timeout}, log: opts.Logger,
		tiers: []Tier{
			{Step: time.Second, Retention: opts.RawRetention},
			{Step: time.Minute, Retention: opts.MinuteRetention},
			{Step: time.Hour, Retention: opts.HourRetention},
		},
	}
	if u.User != nil {
		c.user = u.User.Username()
		c.pass, _ = u.User.Password()
	}
	if err := c.migrate(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func validIdent(s string) bool {
	for _, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return s != ""
}

func (c *ClickHouse) Tiers() []Tier { return c.tiers }

// DropDatabase deletes the history database (tests and uninstall only).
func (c *ClickHouse) DropDatabase(ctx context.Context) error {
	_, err := c.do(ctx, "DROP DATABASE IF EXISTS "+c.db, nil, nil, false)
	return err
}
func (c *ClickHouse) Close() error { c.client.CloseIdleConnections(); return nil }
func (c *ClickHouse) Kind() string { return "clickhouse" }

// do runs one statement. Query parameters are bound server-side
// ({name:Type} placeholders), never interpolated.
func (c *ClickHouse) do(ctx context.Context, query string, params map[string]string, body io.Reader, withDB bool) ([]byte, error) {
	q := url.Values{}
	if withDB {
		q.Set("database", c.db)
	}
	q.Set("output_format_json_quote_64bit_integers", "0")
	for k, v := range params {
		q.Set("param_"+k, v)
	}
	var reqBody io.Reader = strings.NewReader(query)
	if body != nil {
		q.Set("query", query)
		reqBody = body
	}
	u := *c.endpoint
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), reqBody)
	if err != nil {
		return nil, err
	}
	if c.user != "" {
		req.Header.Set("X-ClickHouse-User", c.user)
		req.Header.Set("X-ClickHouse-Key", c.pass)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: %w", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return nil, fmt.Errorf("clickhouse: HTTP %d: %s", resp.StatusCode, msg)
	}
	return out, nil
}

func (c *ClickHouse) exec(ctx context.Context, query string) error {
	_, err := c.do(ctx, query, nil, nil, true)
	return err
}

// -------------------------------------------------------------- schema

const flowKeyCols = `src_ip IPv6, dst_ip IPv6, protocol LowCardinality(String), src_port Int32, dst_port Int32`

// migrations are append-only; each runs once (schema_migrations).
var migrations = []string{
	// 1: raw per-second observations. Ports/ifIndex -1 = absent.
	`CREATE TABLE IF NOT EXISTS flow_seconds (
		ts DateTime('UTC'),
		exporter_id LowCardinality(String),
		input_if Int32, output_if Int32,
		` + flowKeyCols + `,
		src_internal Bool, dst_internal Bool,
		sample_count UInt32,
		estimated_bytes Float64,
		sampling_rate UInt32
	) ENGINE = MergeTree
	PARTITION BY toYYYYMMDD(ts)
	ORDER BY (ts, exporter_id, src_ip, dst_ip, protocol, src_port, dst_port)
	SETTINGS ttl_only_drop_parts = 1`,
	rollupTable("flow_minutes", "toYYYYMM(ts)"),
	rollupTable("flow_hours", "toYear(ts)"),
	rollupView("flow_minutes_mv", "flow_minutes", "flow_seconds", "toStartOfMinute", "ts", "toUInt64(sample_count)"),
	rollupView("flow_hours_mv", "flow_hours", "flow_minutes", "toStartOfHour", "last_ts", "sample_count"),
	`CREATE TABLE IF NOT EXISTS counter_rates (
		at DateTime64(3, 'UTC'),
		exporter_id LowCardinality(String),
		if_index Int32,
		rx_bps Float64, tx_bps Float64,
		interval_seconds Float64
	) ENGINE = MergeTree
	PARTITION BY toYYYYMM(at)
	ORDER BY (at, exporter_id, if_index)`,
}

func rollupTable(name, partition string) string {
	return `CREATE TABLE IF NOT EXISTS ` + name + ` (
		ts DateTime('UTC'),
		exporter_id LowCardinality(String),
		` + flowKeyCols + `,
		input_if SimpleAggregateFunction(any, Int32), output_if SimpleAggregateFunction(any, Int32),
		src_internal SimpleAggregateFunction(any, Bool), dst_internal SimpleAggregateFunction(any, Bool),
		sample_count SimpleAggregateFunction(sum, UInt64),
		estimated_bytes SimpleAggregateFunction(sum, Float64),
		sampling_rate SimpleAggregateFunction(max, UInt32),
		last_ts SimpleAggregateFunction(max, DateTime('UTC'))
	) ENGINE = AggregatingMergeTree
	PARTITION BY ` + partition + `
	ORDER BY (ts, exporter_id, src_ip, dst_ip, protocol, src_port, dst_port)
	SETTINGS ttl_only_drop_parts = 1`
}

// rollupView aggregates the source table into a coarser one. The inner
// subquery renames ts so that the bucket alias cannot shadow it.
func rollupView(name, to, from, bucketFn, lastCol, samplesExpr string) string {
	return `CREATE MATERIALIZED VIEW IF NOT EXISTS ` + name + ` TO ` + to + ` AS
	SELECT ` + bucketFn + `(src_ts) AS ts, exporter_id, src_ip, dst_ip, protocol, src_port, dst_port,
		any(input_if) AS input_if, any(output_if) AS output_if,
		any(src_internal) AS src_internal, any(dst_internal) AS dst_internal,
		sum(samples) AS sample_count, sum(estimated_bytes) AS estimated_bytes,
		max(sampling_rate) AS sampling_rate, max(src_last) AS last_ts
	FROM (SELECT ts AS src_ts, ` + lastCol + ` AS src_last, ` + samplesExpr + ` AS samples,
		exporter_id, src_ip, dst_ip, protocol, src_port, dst_port, input_if, output_if,
		src_internal, dst_internal, estimated_bytes, sampling_rate FROM ` + from + `)
	GROUP BY ts, exporter_id, src_ip, dst_ip, protocol, src_port, dst_port`
}

func (c *ClickHouse) migrate(ctx context.Context) error {
	if _, err := c.do(ctx, "CREATE DATABASE IF NOT EXISTS "+c.db, nil, nil, false); err != nil {
		return err
	}
	if err := c.exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version UInt32, applied_at DateTime('UTC'))
		ENGINE = MergeTree ORDER BY version`); err != nil {
		return err
	}
	out, err := c.do(ctx, "SELECT max(version) AS v FROM schema_migrations FORMAT JSONEachRow", nil, nil, true)
	if err != nil {
		return err
	}
	var cur struct{ V int }
	_ = json.Unmarshal(bytes.TrimSpace(out), &cur)
	for i := cur.V; i < len(migrations); i++ {
		if err := c.exec(ctx, migrations[i]); err != nil {
			return fmt.Errorf("history migration %d: %w", i+1, err)
		}
		if err := c.exec(ctx, fmt.Sprintf("INSERT INTO schema_migrations VALUES (%d, now())", i+1)); err != nil {
			return err
		}
		c.log.Info("history migration applied", "version", i+1)
	}
	// Retention follows the configuration on every start (cheap: existing
	// parts are not rewritten).
	ttl := []struct {
		table, expr string
		d           time.Duration
	}{
		{"flow_seconds", "ts", c.tiers[0].Retention},
		{"flow_minutes", "ts", c.tiers[1].Retention},
		{"flow_hours", "ts", c.tiers[2].Retention},
		{"counter_rates", "toDateTime(at)", c.tiers[2].Retention},
	}
	for _, t := range ttl {
		q := fmt.Sprintf("ALTER TABLE %s MODIFY TTL %s + INTERVAL %d SECOND SETTINGS materialize_ttl_after_modify = 0",
			t.table, t.expr, int64(t.d/time.Second))
		if err := c.exec(ctx, q); err != nil {
			return fmt.Errorf("history retention for %s: %w", t.table, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------- writes

const chTime = "2006-01-02 15:04:05"

func ipv6Text(s string) string {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return "::"
	}
	return netip.AddrFrom16(a.As16()).String() // IPv4 → ::ffff:a.b.c.d
}

func ipFromCH(s string) string {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return s
	}
	return a.Unmap().String()
}

func orMinus1(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

func fromMinus1(v int) *int {
	if v < 0 {
		return nil
	}
	return &v
}

func (c *ClickHouse) WriteFlows(ctx context.Context, rows []FlowRow) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range rows {
		o := r.Obs
		_ = enc.Encode(map[string]any{
			"ts": r.Start.UTC().Format(chTime), "exporter_id": o.ExporterID,
			"input_if": orMinus1(o.InputIfIndex), "output_if": orMinus1(o.OutputIfIndex),
			"src_ip": ipv6Text(o.SrcIP), "dst_ip": ipv6Text(o.DstIP), "protocol": string(o.Protocol),
			"src_port": orMinus1(o.SrcPort), "dst_port": orMinus1(o.DstPort),
			"src_internal": r.SrcInternal, "dst_internal": r.DstInternal,
			"sample_count": o.SampleCount, "estimated_bytes": o.EstimatedBytes, "sampling_rate": o.SamplingRate,
		})
	}
	_, err := c.do(ctx, "INSERT INTO flow_seconds FORMAT JSONEachRow", nil, &buf, true)
	return err
}

func (c *ClickHouse) WriteCounters(ctx context.Context, rows []CounterRow) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range rows {
		_ = enc.Encode(map[string]any{
			"at": r.At.UTC().Format("2006-01-02 15:04:05.000"), "exporter_id": r.ExporterID, "if_index": r.IfIndex,
			"rx_bps": r.RxBps, "tx_bps": r.TxBps, "interval_seconds": r.IntervalSeconds,
		})
	}
	_, err := c.do(ctx, "INSERT INTO counter_rates FORMAT JSONEachRow", nil, &buf, true)
	return err
}

// ----------------------------------------------------------------- reads

func (c *ClickHouse) query(ctx context.Context, q string, params map[string]string, each func(json.RawMessage) error) error {
	out, err := c.do(ctx, q+" FORMAT JSONEachRow", params, nil, true)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := each(line); err != nil {
			return err
		}
	}
	return sc.Err()
}

func rangeParams(start, end time.Time) map[string]string {
	return map[string]string{"start": start.UTC().Format(chTime), "end": end.UTC().Format(chTime)}
}

func parseCHTime(s string) time.Time {
	t, err := time.ParseInLocation(chTime, s, time.UTC)
	if err != nil {
		t, _ = time.ParseInLocation("2006-01-02 15:04:05.000", s, time.UTC)
	}
	return t
}

func (c *ClickHouse) Flows(ctx context.Context, tier Tier, start, end time.Time) ([]MergedFlow, error) {
	table, ok := tierTables[tier.Step]
	if !ok {
		return nil, fmt.Errorf("history: no table for step %s", tier.Step)
	}
	last := "ts"
	if table != "flow_seconds" {
		last = "last_ts"
	}
	q := `SELECT exporter_id, toString(src_ip) AS src, toString(dst_ip) AS dst, protocol, src_port, dst_port,
		any(input_if) AS in_if, any(output_if) AS out_if,
		toUInt64(sum(sample_count)) AS n, sum(estimated_bytes) AS bytes, max(sampling_rate) AS rate,
		toString(max(` + last + `)) AS last
	FROM ` + table + `
	WHERE ts >= {start:DateTime('UTC')} AND ts < {end:DateTime('UTC')}
	GROUP BY exporter_id, src_ip, dst_ip, protocol, src_port, dst_port
	ORDER BY exporter_id, src_ip, dst_ip, protocol, src_port, dst_port`
	var out []MergedFlow
	err := c.query(ctx, q, rangeParams(start, end), func(line json.RawMessage) error {
		var r struct {
			ExporterID string `json:"exporter_id"`
			Src, Dst   string
			Protocol   string
			SrcPort    int `json:"src_port"`
			DstPort    int `json:"dst_port"`
			InIf       int `json:"in_if"`
			OutIf      int `json:"out_if"`
			N          int
			Bytes      float64
			Rate       int
			Last       string
		}
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		out = append(out, MergedFlow{
			Obs: flow.WindowObservation{
				ExporterID: r.ExporterID, InputIfIndex: fromMinus1(r.InIf), OutputIfIndex: fromMinus1(r.OutIf),
				SrcIP: ipFromCH(r.Src), DstIP: ipFromCH(r.Dst), Protocol: flow.Protocol(r.Protocol),
				SrcPort: fromMinus1(r.SrcPort), DstPort: fromMinus1(r.DstPort),
				SamplingRate: r.Rate, SampleCount: r.N, EstimatedBytes: r.Bytes,
			},
			LastStart: parseCHTime(r.Last),
		})
		return nil
	})
	return out, err
}

// Timeline buckets by index from start, so any step aligns with the range.
// The inner query sums per exporter; the middle one keeps one observation
// point per flow key (Dedup); the outer one sums per scope.
func (c *ClickHouse) Timeline(ctx context.Context, tier Tier, start, end time.Time, step time.Duration, dd Dedup) ([]TimelineBucket, error) {
	table, ok := tierTables[tier.Step]
	if !ok {
		return nil, fmt.Errorf("history: no table for step %s", tier.Step)
	}
	p := rangeParams(start, end)
	p["step"] = strconv.FormatInt(int64(step/time.Second), 10)
	p["er"], p["ir"] = chMap(dd.ExternalRank), chMap(dd.InternalRank)
	p["eu"], p["iu"] = strconv.Itoa(dd.UnknownExternal), strconv.Itoa(dd.UnknownInternal)
	q := `SELECT bucket,
		sumIf(b, si AND NOT di) AS out_b, sumIf(n, si AND NOT di) AS out_n,
		sumIf(b, di AND NOT si) AS in_b, sumIf(n, di AND NOT si) AS in_n,
		sumIf(b, si AND di) AS int_b, sumIf(n, si AND di) AS int_n,
		sumIf(b, NOT si AND NOT di) AS tr_b, sumIf(n, NOT si AND NOT di) AS tr_n
	FROM (
		SELECT bucket, tupleElement(pick, 1) AS b, tupleElement(pick, 2) AS n,
			tupleElement(pick, 3) AS si, tupleElement(pick, 4) AS di
		FROM (
			SELECT bucket, argMin((b, n, si, di), (rnk, exporter_id)) AS pick
			FROM (
				SELECT intDiv(toUnixTimestamp(ts) - toUnixTimestamp({start:DateTime('UTC')}), {step:UInt32}) AS bucket,
					exporter_id, src_ip, dst_ip, protocol, src_port, dst_port,
					any(src_internal) AS si, any(dst_internal) AS di,
					sum(estimated_bytes) AS b, toUInt64(sum(sample_count)) AS n,
					if(si AND di,
						if(mapContains({ir:Map(String, UInt16)}, exporter_id), {ir:Map(String, UInt16)}[exporter_id], {iu:UInt16}),
						if(mapContains({er:Map(String, UInt16)}, exporter_id), {er:Map(String, UInt16)}[exporter_id], {eu:UInt16})) AS rnk
				FROM ` + table + `
				WHERE ts >= {start:DateTime('UTC')} AND ts < {end:DateTime('UTC')}
				GROUP BY bucket, exporter_id, src_ip, dst_ip, protocol, src_port, dst_port
			)
			GROUP BY bucket, src_ip, dst_ip, protocol, src_port, dst_port
		)
	)
	GROUP BY bucket ORDER BY bucket`
	var out []TimelineBucket
	err := c.query(ctx, q, p, func(line json.RawMessage) error {
		var raw map[string]float64
		if err := json.Unmarshal(line, &raw); err != nil {
			return err
		}
		out = append(out, TimelineBucket{
			Start:        start.Add(time.Duration(int64(raw["bucket"])) * step),
			InboundBytes: raw["in_b"], OutboundBytes: raw["out_b"], InternalBytes: raw["int_b"], TransitBytes: raw["tr_b"],
			InboundSamples: int(raw["in_n"]), OutboundSamples: int(raw["out_n"]),
			InternalSamples: int(raw["int_n"]), TransitSamples: int(raw["tr_n"]),
		})
		return nil
	})
	return out, err
}

// chMap renders a Map(String, UInt16) query parameter.
func chMap(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(k) + "':" + strconv.Itoa(m[k]))
	}
	b.WriteString("}")
	return b.String()
}

func (c *ClickHouse) CounterSeries(ctx context.Context, start, end time.Time, step time.Duration) ([]CounterBucket, error) {
	p := rangeParams(start, end)
	p["step_ms"] = strconv.FormatInt(step.Milliseconds(), 10)
	q := `SELECT bucket, sum(rx) AS rx, sum(tx) AS tx, max(secs) AS secs FROM (
		SELECT intDiv(toUnixTimestamp64Milli(at) - 1 - toUnixTimestamp64Milli(toDateTime64({start:DateTime('UTC')}, 3)), {step_ms:UInt64}) AS bucket,
			exporter_id,
			sum(rx_bps * interval_seconds) / sum(interval_seconds) AS rx,
			sum(tx_bps * interval_seconds) / sum(interval_seconds) AS tx,
			sum(interval_seconds) AS secs
		FROM counter_rates
		WHERE at > toDateTime64({start:DateTime('UTC')}, 3) AND at <= toDateTime64({end:DateTime('UTC')}, 3) AND interval_seconds > 0
		GROUP BY bucket, exporter_id
	) GROUP BY bucket ORDER BY bucket`
	var out []CounterBucket
	err := c.query(ctx, q, p, func(line json.RawMessage) error {
		var raw map[string]float64
		if err := json.Unmarshal(line, &raw); err != nil {
			return err
		}
		out = append(out, CounterBucket{
			Start: start.Add(time.Duration(int64(raw["bucket"])) * step),
			RxBps: raw["rx"], TxBps: raw["tx"], CoveredSeconds: raw["secs"],
		})
		return nil
	})
	return out, err
}

// Coverage: the oldest data is taken from the finest table that still
// holds it. Rollup rows start at their bucket (a whole minute/hour), so a
// coarser table only counts when it reaches further back than a finer one
// (after the finer table's TTL expired).
func (c *ClickHouse) Coverage(ctx context.Context) (time.Time, time.Time, bool, error) {
	q := `SELECT
		(SELECT count() FROM flow_seconds) AS n_raw,
		(SELECT count() FROM flow_minutes) AS n_min,
		(SELECT count() FROM flow_hours) AS n_hour,
		toString((SELECT min(ts) FROM flow_seconds)) AS raw_min,
		toString((SELECT max(ts) FROM flow_seconds)) AS raw_max,
		toString((SELECT min(ts) FROM flow_minutes)) AS min_min,
		toString((SELECT max(last_ts) FROM flow_minutes)) AS min_last,
		toString((SELECT min(ts) FROM flow_hours)) AS hour_min,
		toString((SELECT max(last_ts) FROM flow_hours)) AS hour_last`
	var counts struct {
		NRaw  int `json:"n_raw"`
		NMin  int `json:"n_min"`
		NHour int `json:"n_hour"`
	}
	var t map[string]any
	err := c.query(ctx, q, nil, func(line json.RawMessage) error {
		if err := json.Unmarshal(line, &counts); err != nil {
			return err
		}
		return json.Unmarshal(line, &t)
	})
	if err != nil || counts.NRaw+counts.NMin+counts.NHour == 0 {
		return time.Time{}, time.Time{}, false, err
	}
	var earliest, latest time.Time
	str := func(k string) string { v, _ := t[k].(string); return v }
	use := func(min, last string, step time.Duration) {
		m := parseCHTime(str(min))
		if earliest.IsZero() || m.Before(floorTo(earliest, step)) {
			earliest = m
		}
		if l := parseCHTime(str(last)); l.After(latest) {
			latest = l
		}
	}
	if counts.NRaw > 0 {
		use("raw_min", "raw_max", time.Second)
	}
	if counts.NMin > 0 {
		use("min_min", "min_last", time.Minute)
	}
	if counts.NHour > 0 {
		use("hour_min", "hour_last", time.Hour)
	}
	return earliest, latest, true, nil
}
