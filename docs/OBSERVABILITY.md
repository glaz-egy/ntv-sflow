# Observability

The visualization platform should monitor itself.

## 1. Collector metrics

Implemented in `internal/collector/metrics.go` and served at `GET /metrics` on `collector.metrics_listen` (default `:9102`):

- `ntv_collector_datagrams_received_total`
- `ntv_collector_datagrams_invalid_total`, `ntv_collector_decoder_errors_total{kind}`
- `ntv_collector_datagrams_dropped_total` (queue full) and `ntv_collector_datagrams_rejected_total` (source not allowed)
- `ntv_collector_datagrams_lost_estimate` (from agent sequence numbers, D-048)
- `ntv_collector_agent_restarts_total`, `ntv_collector_agent_reported_drops_total`
- `ntv_collector_flow_samples_total`, `ntv_collector_counter_samples_total`
- `ntv_collector_unsupported_samples_total`, `ntv_collector_unsupported_records_total`
- `ntv_collector_malformed_samples_total`, `ntv_collector_malformed_records_total`
- `ntv_collector_non_ip_samples_total`, `ntv_collector_invalid_sampling_rate_total`
- `ntv_collector_ingestion_queue_depth`, `ntv_collector_ingestion_queue_capacity`
- `ntv_collector_processing_lag_seconds`

Labels should be bounded; avoid IP-heavy cardinality where unnecessary.

## 2. Aggregation metrics

- active_windows
- observations_processed_total
- aggregates_emitted_total
- enrichment_failures_total
- geo_cache_hits/misses
- device_resolution_hits/misses

## 3. API metrics

- requests_total
- request_duration
- websocket_connections
- websocket_messages_sent
- websocket_backpressure/drop counts
- historical_query_duration

## 4. Database metrics

History recorder (`/metrics` on the API, D-059):
- `ntv_history_rows_written_total{kind="flow"|"counter"}`
- `ntv_history_rows_dropped_total{reason="queue_full"|"retry_overflow"}`: rows not persisted. History has gaps, live is unaffected.
- `ntv_history_write_errors_total`: failed writes (rows are retried from a bounded buffer).

Monitor:
- ClickHouse ingest
- query latency
- disk
- merge backlog
- PostgreSQL connections
- database errors

## 5. UI status

Expose:
- collector status
- last datagram
- last aggregate
- live/mock mode
- stale state

## 6. Logging

Structured logs recommended.

Suggested fields:
- component
- exporter_id
- operation
- error_code
- duration
- sequence where relevant

Do not log every flow at normal level.
