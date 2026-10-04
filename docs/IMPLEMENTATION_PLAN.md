# Implementation Plan

Status as of 2026-10-05 (Milestones A–E complete: the MVP definition of done is met; history is persisted and replayable). Companion to `TASKS.md` (backlog) and `DECISIONS.md` (D-021+ were added while executing this plan).

## 1. Design-document review

### 1.1 Contradictions found (and resolutions)

| # | Where | Contradiction | Resolution |
|---|-------|---------------|------------|
| C1 | API.md §3 vs §6 | Globe rates are `Measurement` objects (`{value, measurement_kind}`) but Home `rx_bps`/`tx_bps`/`estimated_bps` are bare numbers, contradicting DATA_MODEL §7 and rule 5. | Every rate is a `Measurement` (D-023). API.md updated. |
| C2 | API.md §11 vs TASKS M5 | WebSocket is `WS /ws` in API.md but `/api/v1/ws` in TASKS. | `/api/v1/ws` (D-031). |
| C3 | config.example.yaml vs MOCK_DATA §6 | Default `min_bps: 1000000` would hide the required "IoT → small periodic external traffic". | UI default threshold is 0; small flows render very thin (D-028). |
| C4 | DATA_MODEL §2 vs API.md §6 | Device id is a UUID in the data model, but the API example uses `dev_pc01`. | Device ids are opaque strings in the contract; UUIDs in persistence; mock uses readable opaque slugs (D-027). |
| C5 | DATA_MODEL `TopologyLink.source` vs API.md §7 | `source` means *evidence source* (lldp/manual…) but is also the natural name for a link endpoint. | Endpoints are `source_node_id`/`target_node_id`; evidence is `evidence` (D-032). |
| C6 | MOCK_DATA §2 vs HOME_NETWORK_VIEW §4 | Mock inventory has `tablet-01` but there is no `tablet` device type. | Mapped to `smartphone`; taxonomy unchanged (D-030). |
| C7 | ARCHITECTURE §3 vs DEVELOPMENT §4 vs CLAUDE.md | Three different Go package lists (`internal/api` vs `internal/httpapi`; `sflow`/`counters` missing from CLAUDE.md). | Union, with `internal/httpapi` (see §3). |
| C8 | TASKS order vs start prompt | TASKS puts the app shell (M4) after Globe/Home; the start prompt builds the shell with Globe. | Shell built first because both views need it. |

### 1.2 Missing assumptions (now recorded)

- Where a grouped Globe marker sits (country/ASN have no single location) → D-022.
- How to pick one observation point when several exporters see the same flow → D-024.
- Shared contract mechanism before the Go API exists → D-021.
- Where mock mode runs (browser vs Go) and how parity is kept → D-025.
- URL/state model and what each parameter means in each view → D-026.
- Identity of unresolved endpoints, the Internet aggregate and destination nodes in Home → D-027.
- Summary-bar semantics ("Total", "Download") and their measurement basis → D-034.
- Default Globe grouping (spec left it open) → D-029.

## 2. Plan

1. **Scaffold** — pnpm workspace, Next.js 16 app in `apps/web`, Tailwind 4, shadcn/ui, Vitest, Prettier, ESLint. ✅
2. **Contracts** — wire DTOs in `apps/web/src/contracts` mirroring `docs/API.md`. ✅
3. **Mock mode** — deterministic engine that simulates per-exporter sFlow sampling, plus a reference pipeline (CIDR classification, observation de-dup, device resolution, GeoIP) that serves both views from one attributed-flow set. ✅
4. **Shell + Globe View** — globe.gl, arcs/markers updated in place, filters, inspector, unknown-location panel, reduced motion. ✅
5. **Home View** — React Flow, Traffic/Topology/Hybrid, deterministic layout, device/edge inspectors. ✅
6. **Cross-view navigation** — URL-encoded investigation context with pure transition functions. ✅
7. **Tests** — CIDR/IP, sampling math, counters, de-dup, determinism, cross-view consistency, URL state, layout stability. ✅ (71 tests)
8. **Go backend skeleton (Milestone B)** — OpenAPI source of truth, Go port of the mock with exact parity, REST + WebSocket API, HTTP provider in the frontend, Docker images. ✅
9. **sFlow v5 collector (Milestone C)** — decoder, normalization, UDP runtime with backpressure and metrics, generator for end-to-end tests. ✅
10. **Live mode (Milestone D)** — collector output aggregated into windows and served by the unchanged projections; inventory, exporters, GeoIP. ✅
11. **Milestone E** — persistence and history (D-059): stored per-second observations rebuilt into historical windows, timeline and replay. ✅ PostgreSQL deferred (D-060).

## 3. Repository tree

`✅` exists now, `○` planned.

```text
.
├── CLAUDE.md, README.md, .env.example, .gitignore
├── package.json, pnpm-workspace.yaml         ✅ root scripts proxy to apps/web
├── .claude/launch.json                       ✅ dev server config
├── apps/
│   └── web/                                  ✅ Next.js 16 + TS + Tailwind 4 + shadcn/ui
│       └── src/
│           ├── app/
│           │   ├── page.tsx                  ✅ → /globe
│           │   └── (views)/                  ✅ shared layout keeps provider alive across views
│           │       ├── globe/page.tsx
│           │       └── home/page.tsx
│           ├── contracts/                    ✅ wire DTOs (source of truth until OpenAPI, D-021)
│           ├── components/
│           │   ├── shell/                    ✅ header, tabs, status, workspace panels
│           │   ├── common/                   ✅ Rate (measurement-aware), fields
│           │   ├── globe/                    ✅ canvas, view, destination inspector
│           │   ├── home/                     ✅ canvas, nodes/edges, view, inspectors
│           │   └── ui/                       ✅ shadcn primitives
│           └── lib/
│               ├── data/                     ✅ provider interface, mock provider, hooks
│               ├── mock-backend/             ✅ engine, scenarios, inventory, geodb, pipeline, server
│               ├── state/                    ✅ URL context, transitions, UI store
│               ├── format/                   ✅ "≈" formatting for estimates
│               └── viz/                      ✅ scales, colours, Home layout
├── api/openapi.yaml                          ✅ contract source of truth (D-021)
├── go.mod                                    ✅ module network-traffic-visualizer (Go 1.27)
├── Dockerfile, Dockerfile.backend                 ✅ web image / Go image (api, collector, sflow-gen)
├── cmd/
│   ├── api/                                  ✅ REST + WebSocket server
│   ├── collector/                            ✅ sFlow v5 UDP collector
│   └── sflow-gen/                            ✅ sends sFlow generated from the mock
├── internal/
│   ├── contract/                             ✅ wire DTOs
│   ├── config/                               ✅ defaults → YAML → env, validation
│   ├── flow/                                 ✅ normalized flow domain
│   ├── devices/                              ✅ registry & identities
│   ├── topology/                             ✅ links with evidence
│   ├── enrichment/                           ✅ CIDR (netip), GeoIP lookup interface
│   ├── counters/                             ✅ counter → rate
│   ├── aggregation/                          ✅ de-dup, attribution, destination keys
│   ├── projection/                           ✅ Globe/Home/Topology/Device/Flows views
│   ├── mock/                                 ✅ engine + backend, golden parity with TS
│   ├── realtime/                             ✅ WebSocket fan-out
│   ├── httpapi/                              ✅ handlers, validation, CORS, contract tests
│   ├── sflow/                                ✅ v5 decode/encode (fuzzed)
│   ├── packet/                               ✅ Ethernet/VLAN/IPv4/IPv6/TCP/UDP header parsing (fuzzed)
│   ├── collector/                            ✅ UDP runtime, normalization, metrics, sinks
│   ├── sflowgen/                             ✅ mock → sFlow datagrams
│   ├── history/                              ✅ stores (memory, ClickHouse), recorder, historical windows, timeline
│   └── (storage/)                            ○ PostgreSQL when inventory gets a writer (D-060)
├── configs/config.example.yaml
├── deploy/docker-compose.dev.yml
└── docs/
```

## 4. Milestones

### Milestone A — Mock-mode MVP (done)

- [x] Workspace + Next.js + Tailwind + shadcn/ui + Vitest + Prettier + ESLint
- [x] Shared contracts (`apps/web/src/contracts`)
- [x] Deterministic mock engine (seed/scenario/speed), all six documented scenarios
- [x] Per-exporter sampling simulation; counter-based WAN totals with first-sample fallback
- [x] Reference pipeline: CIDR (v4/v6), observation de-dup, device resolution, GeoIP/ASN grouping
- [x] App shell: tabs, MOCK/LIVE/PAUSED/STALE status, settings, pause/resume, collapsible panels
- [x] Globe View with arcs, markers, direction cues, grouping/direction/source/protocol/threshold/top-N
- [x] Destination inspector with sources, ports, members, observation points, "Open in Home Network"
- [x] Home View: Traffic/Topology/Hybrid, evidence-styled topology, logical traffic edges
- [x] Device and edge inspectors with "Show external destinations in Globe", focus
- [x] URL-shareable investigation context, cross-view transitions
- [x] Tests: 71 passing

### Milestone B — Go backend skeleton (done)

- [x] Go toolchain; `go.mod` at repo root
- [x] `internal/config` (defaults → YAML → env) with validation and unknown-key rejection
- [x] `api/openapi.yaml`; TS types generated from it (`pnpm gen:api`) with a drift test
- [x] `internal/mock`: Go port of scenarios + engine; exact parity with TS via 148 golden cases (D-039)
- [x] `cmd/api`: `/status`, `/globe`, `/globe/destinations/{key}`, `/home/traffic`, `/home/topology`, `/devices`, `/devices/{id}`, `/flows`, `/ws`, `/healthz`
- [x] Parameter validation with `ApiError` bodies; every response validated against the spec in tests
- [x] Frontend `HttpDataProvider` (REST + WebSocket with reconnect); `NEXT_PUBLIC_DATA_MODE=api`
- [x] `Dockerfile.api` + compose `api` service; web image switchable to API mode
- Tests: 78 (TS) + 23 (Go)

### Milestone C — sFlow collector (done)

- [x] `internal/sflow`: bounds-checked v5 decoder (flow/counter samples, compact and expanded; raw header, sampled IPv4/IPv6, extended switch, generic interface counters), encoder for tests and the generator
- [x] `internal/packet`: Ethernet/802.1Q/QinQ/IPv4/IPv6 (+ extension headers)/TCP/UDP parsing
- [x] Normalization to `flow.Sample` and `counters.Observation`; `counters.Tracker` for rates
- [x] UDP runtime: bounded queue with drop accounting, parallel decode workers, source allow-list, rate-limited logs
- [x] Prometheus metrics (`/metrics`), reordering-safe loss estimate, agent restart detection
- [x] `cmd/collector` (summary log, optional bounded JSON-line debug output), `cmd/sflow-gen`
- [x] Tests: spec-layout byte fixture, round trips, every-prefix truncation, unit tests (race detector), end-to-end mock → UDP → collector exact match; fuzzing (~13M executions each, no crashes)
- [x] Docker: one Go image; compose `collector` service and `sflow-gen` demo profile

Not yet: collector output is not fed into the API. That is Milestone D.

### Milestone D — Live mode (done)

- [x] `internal/live`: per-second buckets → sliding window of completed seconds (D-054), late-sample accounting, per-second key limit
- [x] Exporter mapping (agent → configured exporter; unknown agents discovered), observation policy from inventory
- [x] WAN Download/Upload from boundary-interface counters (D-055)
- [x] `internal/inventory`: devices, networks, exporters, topology, GeoIP overrides; strict validation (D-009 enforced)
- [x] `internal/enrichment`: MaxMind City/ASN adapter, override chain, bounded cache; country placement fallback `country_dominant` (D-056)
- [x] `app.mode: live`: API embeds the collector (D-053), fails fast on bind errors, `/metrics`
- [x] Frontend: `SFLOW` badge, "Waiting for sFlow" state. No visualization code changed (DoD 8)
- [x] Proof: sFlow from the generator → collector → live aggregator produces **exactly** the mock's Globe/Home/Topology/Device/Flows responses (3 scenarios), plus a sensitivity test that fails on one lost datagram
- [x] Docker: `api-live` service (`--profile live`), demo with `sflow-gen`
- Tests: 78 (TS) + 65 Go tests incl. subtests (live, inventory, enrichment, collector, contract tests with live responses validated against OpenAPI)

### Milestone E — Persistence and history

- [x] `internal/history`: `Store` interface; `Memory` (default, bounded) and `ClickHouse` (HTTP interface, no driver dependency)
- [x] ClickHouse schema: per-second rows, 1 m / 1 h rollups by materialized views, boundary counters; migrations; TTL from `history.*`
- [x] Asynchronous, bounded recorder (drops are counted; failed writes retried from a bounded buffer); metrics
- [x] Historical windows: `start`/`end` on every view endpoint plus `/flows` with `cursor`; tier choice and alignment; averaging trimmed to stored data
- [x] `GET /history/timeline`: per-scope sampled estimates de-duplicated across observation points, plus boundary counters
- [x] Proof: a historical window over a live window's seconds yields identical Globe/Home/Flows/Destination/Device responses, for all mock scenarios (memory and ClickHouse) and for sFlow → collector → live → history
- [x] UI: timeline (download/upload, counter lines, gaps), click/drag/keyboard selection, step, replay 0.5–4×, `at`/`span` in the URL, "History" badge; the in-browser mock never answers history with live data
- [x] Compose `clickhouse` service, Kubernetes `components/clickhouse`
- [ ] PostgreSQL for inventory/settings — deferred (D-060)

## 5. Known gaps / follow-ups

- Frontend component/e2e tests (Globe selection, Home selection, Globe↔Home in a real browser) are not yet automated; logic is covered by unit tests on the pure state transitions and the mock backend.
- History uses the current inventory for attribution (D-059): renamed/re-addressed devices appear with today's metadata.
- Recording gaps inside a historical window count as no traffic (the timeline shows them).
- Rollups keep ephemeral ports; revisit after measuring ingest volume.
- Device discovery (DHCP/ARP/LLDP/WLC) is not automated; devices come from the inventory file.
- WebSocket subscription filters are accepted but not used yet: the server sends only the global `window_update`, and clients re-query.
- The mock's `update_interval_seconds` and `window_seconds` are fixed at 1 s and 5 s; `app.update_interval` controls only the server's clock tick.
- Home device-type filter UI is single-select (the contract supports a list).
- Time range/history selection is not in the URL yet (live-only MVP).
