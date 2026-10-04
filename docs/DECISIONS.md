# Decisions and Assumptions

This document records choices that should not be silently re-decided.

## D-001 — Product is visualization-first
Status: Accepted

Main UI:
- Globe
- Home graph

Charts/tables are supporting UI.

## D-002 — Two views share investigation context
Status: Accepted

Filters and selected entities should survive mode switching when semantically compatible.

## D-003 — Globe is 3D
Status: Accepted

Do not substitute a 2D map as the main Globe experience.

## D-004 — Home has Traffic/Topology/Hybrid
Status: Accepted

Traffic relationships and physical topology are different data concepts and must not be conflated.

## D-005 — sFlow estimates are estimates
Status: Accepted

Flow sample derived throughput is labeled sampled estimate.

## D-006 — Counter samples preferred for interface totals
Status: Accepted

WAN totals should use configured boundary/interface counters where possible.

## D-007 — Multiple exporters can observe same communication
Status: Accepted

No naive global sum across observation points.

## D-008 — Configurable internal CIDRs
Status: Accepted

RFC1918 alone is insufficient.
Support owned public prefixes and IPv6.

## D-009 — IP is not permanent identity
Status: Accepted

Devices use UUID-like identities.

## D-010 — GeoIP is approximate
Status: Accepted

Never claim that map arcs represent the actual Internet route.

## D-011 — Preferred initial frontend libraries
Status: Accepted unless implementation evidence suggests otherwise

- Globe: globe.gl
- Home: React Flow

## D-012 — Preferred backend language
Status: Accepted

Go.

## D-013 — Storage split
Status: Accepted target architecture

- PostgreSQL metadata
- ClickHouse history
- Redis optional live acceleration

## D-014 — Mock-first implementation
Status: Accepted

Frontend must be useful before live collector is complete.

## D-015 — Default UI cadence
Status: Assumption

1–5 seconds, configurable.

## D-016 — Default visible Globe arcs
Status: Assumption

~100 maximum initial target.

## D-017 — Default visible Home nodes
Status: Assumption

~100–300 initial target.

## D-018 — Exact product name
Status: Open

"Network Traffic Visualizer" is a working name only.

## D-019 — Authentication
Status: Deferred

MVP may run on a trusted management network. Add authentication before exposed deployment.

## D-020 — License
Status: Open

Do not choose a repository license without explicit project decision.

---

Decisions below were recorded while implementing the mock-mode MVP (see `IMPLEMENTATION_PLAN.md` §1 for the doc review that motivated them).

## D-021 — Contract source of truth
Status: Accepted (implemented in Milestone B)

`api/openapi.yaml` (OpenAPI 3.0.3) is the single source of truth.
- TypeScript: `apps/web/src/contracts/generated/openapi.ts` is generated (`pnpm gen:api`). `src/contracts/*.ts` only alias the generated schemas, and `openapi-sync.test.ts` fails when the generated file is stale.
- Go: the `internal/contract` structs are hand-written. `internal/httpapi` tests validate every endpoint's real responses against the spec (kin-openapi).

OpenAPI 3.0 is used, not 3.1, because the Go validator's 3.0 support is complete. Nullable references are written as `nullable: true` + `allOf: [$ref]`.

## D-022 — Marker location for grouped destinations
Status: Accepted

GeoIP gives per-IP coordinates only. Markers for groups use:
- `ip`: the IP's GeoIP coordinate (`basis: geoip`)
- `city`: the city coordinate (`basis: city`)
- `country`: a configured representative point per country (`basis: country_anchor`)
- `asn`: the location contributing the most traffic to that ASN in the window (`basis: asn_dominant`)

The basis is exposed in the contract and shown in the inspector. A group with no located member has `location: null` and is listed under "Unknown location". It is never given a placeholder coordinate.

## D-023 — Every rate carries a Measurement
Status: Accepted

All bps values in API responses are `Measurement` objects (`value`, `unit`, `measurement_kind`, `window_seconds` / `interval_seconds`, `sample_count`). This replaces the bare numbers in the earlier Home examples. The UI prefixes sampled estimates with "≈" and tags them `est`; counter values are tagged `ctr`.

## D-024 — Observation-point de-duplication policy
Status: Accepted (MVP)

When several exporters observe the same unidirectional flow key in a window, exactly one observation is used. It is chosen by exporter role preference per scope:
- crossing the boundary: `boundary` → `core` → `access`
- internal only: `core` → `access` → `boundary`

Ties are broken by exporter id. Values are never summed across exporters, and the largest value is never picked, because that biases estimates upward. All observing exporters are still reported as observation points (`used_for_aggregate` true/false). Role preference becomes configuration in the Go implementation.

## D-025 — Mock mode runs in the browser by default
Status: Accepted

`apps/web/src/lib/mock-backend` is an in-process stand-in for the API that returns the same DTOs, so mock mode needs no backend (rule 13). It is also the reference implementation for the Go pipeline. The Go mock adapter (Milestone B) must reproduce it. Parity is enforced with golden JSON fixtures produced by the TS mock at fixed (seed, scenario, tick).

## D-026 — Investigation context lives in the URL
Status: Accepted

Parameters: `src`, `dst`, `proto`, `min`, `top`, `g`, `dir`, `mode`, `scope`, `sel`, `vlan`, `types`, `inactive`.
- `src` is the Globe source-device filter and also Home's focused device.
- `dst` is the destination key: the selected destination in Globe and the destination filter in Home.
- A `dst` implies its grouping, and changing the grouping clears an incompatible `dst`.
- Home → Globe with a *different* device clears `dst`, because it may not be attributable to the new source.
- Invalid values fall back to defaults and never throw.

Pause state, panel state, animation and mock scenario are per-browser UI state, not URL state.

## D-027 — Node identities in Home
Status: Accepted

- Resolved devices: opaque persistent ids (UUIDs in persistence; readable slugs such as `dev_pc01` in mock).
- Unresolved internal endpoints: temporary `ep:<ip>`, rendered as "Unresolved device", mergeable later.
- External aggregate: `internet`.
- A selected destination split out of the Internet aggregate: `dst:<destination key>`.

## D-028 — Default minimum traffic threshold
Status: Assumption

The UI defaults to 0 bps so the small IoT traffic required by `MOCK_DATA.md` is visible (rendered very thin). `visualization.*.min_bps` in config remains the intended server-side default for live mode and will be revisited with real data.

## D-029 — Default Globe grouping
Status: Assumption

ASN, matching `config.example.yaml`. Configurable.

## D-030 — Tablet devices
Status: Assumption

The device taxonomy has no `tablet`. Mock `tablet-01` uses `smartphone`. Add a type only by explicit decision.

## D-031 — WebSocket path and window events
Status: Accepted

`/api/v1/ws`. Besides the documented events, the server emits `window_update` (window end + status) once per aggregate window. Clients re-query only what they display. Raw samples are never forwarded.

## D-032 — Home edge and topology link shapes
Status: Accepted

Traffic edges connect an unordered endpoint pair, with `forward_bps` (source→target) and `reverse_bps`. They are logical relationships and are drawn curved, never along topology links. Topology links use `source_node_id`/`target_node_id` plus `evidence` (manual/lldp/cdp/wlc/inferred) and `confidence`. `inferred` links render dotted.

## D-033 — Home layout
Status: Accepted (MVP)

A deterministic tiered layout: Internet, then router/firewall, then switches/APs, then endpoints in VLAN column blocks. Positions depend only on node identity and metadata, never on traffic. Transient nodes (unresolved endpoints, destination nodes) are placed so that existing nodes never move. No force layout in MVP.

## D-034 — Summary metrics
Status: Accepted

Globe Download/Upload/Total and Home "WAN total" use boundary-interface counter rates when available (`basis: boundary_counter`). They fall back to de-duplicated sampled estimates (`basis: sampled_sum`), for example before the second counter poll. Home "Internal" is always a sampled estimate. Summary values are global and are not affected by view filters.

## D-035 — Mock sampling simulation
Status: Accepted

Each conversation has a smooth "true" rate. Each observing exporter draws Poisson sample counts at its own 1:N rate, and estimates are `samples × packet_length × N`. Small flows therefore have realistically noisy or missing estimates and low `sample_count`. The boundary WAN counter is the sum of the true external bytes plus background traffic, so counter and estimate differ the way they do in production.

## D-036 — Client-only rendering of the live app
Status: Accepted

The views render only after hydration. Live data, clocks and status make server-rendered HTML meaningless and cause hydration mismatches.

## D-037 — Globe land rendering without external assets
Status: Accepted

Land is drawn as hex dots from the bundled `world-atlas` 110m dataset. No texture CDN is used, so the app works offline. Polygons that H3 cannot convert are drawn as flat polygons instead of being dropped.

## D-038 — Go toolchain availability
Status: Resolved

Go 1.27 is required (`go.mod`). Docker builds use `golang:1.27-alpine`.

## D-039 — Exact TS ↔ Go parity of the mock
Status: Accepted

The Go mock (`internal/mock`, `internal/aggregation`, `internal/projection`) reproduces the TS mock exactly. `internal/mock/testdata/golden.json` is written by the TS mock (`pnpm update:golden`). Go replays the same 148 requests and must produce identical JSON. The tolerance is 1e-9 relative so other CPU architectures still pass; on darwin/arm64 the match is bit-exact.

Rules that make this possible:
- The mock's PRNG and hash use 32-bit integer arithmetic only.
- Floats use only + − × ÷ √ and `exp`. No cos/log, which can differ in the last bit between V8 and Go.
- In Go, every product that feeds an addition is wrapped in `float64(...)`, so the compiler cannot fuse it into an FMA (JS never fuses).
- Sorting compares code points (`compareStrings`), never `localeCompare`.
- Iteration order is insertion order in both languages; Go uses ordered slices, not map iteration.

Any change to the TS mock requires `pnpm update:golden`. Otherwise the TS golden test fails.

## D-040 — Frontend data transport selection
Status: Accepted

`NEXT_PUBLIC_DATA_MODE=mock` (default) runs the mock in the browser. `NEXT_PUBLIC_DATA_MODE=api` uses `HttpDataProvider` (REST + WebSocket) against `NEXT_PUBLIC_API_BASE_URL`. Views are unchanged: both providers implement `TrafficDataProvider` and return the same DTOs. Whether data is mock or live is reported by the server in `status.mode`, independent of the transport. In API transport the mock scenario is a server setting, and the UI only displays it.

## D-041 — Browser access to the API
Status: Accepted (MVP)

The browser calls the API directly, with no Next.js proxy, because WebSocket proxying through Next is unreliable.
- CORS uses an allow-list (`api.cors_allowed_origins`, default `http://localhost:3000`).
- WebSocket origins are checked against the same list.
- At most 100 WebSocket clients are accepted. A slow client drops events instead of blocking others, and the next `window_update` supersedes a dropped one.

Authentication is still deferred (D-019).

## D-042 — Go package layout
Status: Accepted

- `internal/contract`: wire DTOs only.
- `internal/flow`, `internal/devices`, `internal/topology`: domain types.
- `internal/enrichment`: CIDR classification (`net/netip`) and GeoIP lookup interface.
- `internal/counters`: counter→rate math.
- `internal/aggregation`: observation de-dup, attribution, destination keys.
- `internal/projection`: source-agnostic API views from one attributed-flow set.
- `internal/mock`: deterministic engine and `Backend`.
- `internal/realtime`: WebSocket fan-out.
- `internal/httpapi`: validation, handlers, CORS, WebSocket.
- `internal/config`: defaults → YAML → env.

The future live pipeline will implement the same `httpapi.Source` interface (`Snapshot()`, `Status()`) that the mock backend implements now.

## D-043 — `/flows` searches the current window only
Status: Assumption (until persistence)

`GET /api/v1/flows` filters the current window's attributed flows. Time ranges and cursors arrive with ClickHouse (Milestone D).

## D-044 — Configuration strictness
Status: Accepted

Unknown YAML keys are errors, so a typo cannot silently fall back to a default. All validation problems are reported together. `app.mode: live` is rejected until the collector exists, rather than starting a server with no data.

## D-045 — Sampled packet length
Status: Accepted

`estimated_bytes = frame_length × sampling_rate`, where `frame_length` comes from the raw packet header record. That is the original frame length on the wire as reported by the exporter; many exporters include the FCS in it. When an exporter sends no raw header, the IP length from `sampled_ipv4`/`sampled_ipv6` is used instead. Estimates therefore count L2 bytes when available. They remain estimates (D-005).

## D-046 — Observation timestamps
Status: Accepted

sFlow datagrams carry no wall-clock time, only agent uptime. `observed_at` is the collector's receive time. Uptime is used only to detect agent restarts.

## D-047 — Collector backpressure
Status: Accepted

The UDP receive loop only copies datagrams into a bounded queue. Decoding runs on worker goroutines. When the queue is full, datagrams are dropped and counted (`ntv_collector_datagrams_dropped_total`) rather than blocking the socket, because a blocked socket would drop silently in the kernel. Heavy enrichment (GeoIP, devices) never runs in the collector hot path.

## D-048 — Datagram loss estimate
Status: Accepted

Loss is estimated per agent as `(highest − first + 1) − received` sequence numbers since the agent's last restart. This is a gauge, not a gap counter, so reordering between parallel workers does not create false losses. A restart is detected when uptime goes backwards by more than 60 s, or the sequence number jumps far outside the seen range.

## D-049 — Exporter identity in the collector
Status: Accepted (until exporter configuration exists)

The observation point is `<agent address>/<sub-agent id>`, from the datagram payload and not the UDP source (which may be NATed). `collector.allowed_sources` filters by UDP source address. Mapping agents to configured exporters (name, role, boundary interfaces) arrives with aggregation (Milestone D).

## D-050 — Unsupported sFlow content
Status: Accepted

Unknown sample formats, unknown record formats and enterprise ≠ 0 structures are skipped via their length fields and counted. Malformed records inside a well-framed sample are skipped, and the sample is kept. A datagram is rejected only when its framing is unusable. Non-IP samples (ARP, MPLS, …) and samples with `sampling_rate = 0` are dropped and counted, never guessed.

## D-051 — sFlow generator for testing
Status: Accepted

`cmd/sflow-gen` converts the mock engine's sampled packets into real sFlow v5 datagrams: Ethernet/IP/L4 raw headers, plus boundary-interface counter samples every 5 s. The end-to-end test sends them over UDP and checks that the collector reproduces the mock's per-flow sample counts and estimated bytes exactly. Real exporters remain the acceptance target. The generator exists because no hardware is available in CI.

## D-052 — Canonical observation order
Status: Accepted

`aggregation.Attribute` sorts its input by (flow key, exporter id) before grouping, in both TS and Go. Network arrival order is arbitrary, and results (tie-breaks, the "first flow" of a group) must not depend on it. This is what makes live output reproducible and comparable to the mock.

## D-053 — Live mode process layout
Status: Accepted (MVP)

With `app.mode: live`, the API process embeds the sFlow collector. The flow is: UDP → `collector` → `live.Source` (per-second buckets, sliding window) → `aggregation` → `projection` → REST/WS. The packages stay separate, and `cmd/collector` still runs standalone (logs and metrics). A queue-based split into separate processes can come later without touching projections. If the UDP socket cannot be bound, startup fails, rather than serving an API that waits forever.

## D-054 — Live windows use completed seconds
Status: Accepted

A window covers the last W completed seconds `(tick−1−W, tick−1]`. The current, partially received second is excluded, so it cannot bias estimates low. Samples older than every window still to be served are dropped and counted (`ntv_live_samples_late_total`). Samples arriving late but still inside the window are included. `observed_at` is the receive time (D-046).

## D-055 — Live inventory and exporters
Status: Accepted

`inventory_file` (YAML) supplies devices, networks, exporters, the observation policy, known topology and GeoIP overrides. All of it is optional and validated strictly:
- unknown keys are errors;
- device ids may not be IP addresses (D-009);
- an address cannot belong to two devices;
- topology endpoints must exist.

Agents not listed under `exporters` still appear: their id is `agent/sub-agent`, their name is the agent address, and their role is unknown, so they get the lowest de-dup preference. WAN Download/Upload use counter samples of `boundary` exporters' `boundary_if_index`, summed across boundary exporters and only while fresh. Otherwise the summary falls back to sampled estimates (D-034).

## D-056 — GeoIP sources and country placement
Status: Accepted

Lookup order: inventory `geo_overrides` (most specific prefix) → MaxMind GeoLite2/GeoIP2 City + ASN → unknown. Results go into a bounded cache. Missing database files are a warning, not an error: destinations then appear under "Unknown location". Countries without a configured anchor are placed at their highest-traffic located member (`location.basis: country_dominant`), the same rule as for ASNs.

## D-057 — Milestone split: live mode before persistence
Status: Accepted

Milestone D delivers live mode, which is the MVP definition of done (CLAUDE.md items 7–8). PostgreSQL/ClickHouse persistence and history move to Milestone E, because no history UI consumes them yet.

## D-058 — Kubernetes deployment shape
Status: Accepted (MVP)

`deploy/k8s` (Kustomize) has three variants: `base` (mock API + web), `overlays/live` (embedded collector, D-053) and `overlays/demo` (live + `sflow-gen`). The guide is `docs/KUBERNETES.ja.md`.
- The API runs as one replica with the `Recreate` strategy, because live windows and WebSocket clients live in process memory. Scaling out needs the queue-based split that D-053 defers.
- UI and API share one Ingress host (`/api` goes to the API), so the CORS and WebSocket origin allow-list (D-041) holds just that origin. The web image is built per host, because `NEXT_PUBLIC_API_BASE_URL` is inlined at build time.
- sFlow enters through a UDP LoadBalancer Service with `externalTrafficPolicy: Local`, which keeps the exporter source IP for `collector.allowed_sources`. Exporter identity still comes from the agent address (D-049).
- GeoIP databases are optional and mounted from a PVC (`components/geoip`), because they exceed the ConfigMap size limit. PostgreSQL and ClickHouse are not deployed until persistence exists (D-057).
- `/healthz` and `/metrics` are not routed through the Ingress. There is no authentication yet (D-019), so expose the Ingress on trusted networks only.
