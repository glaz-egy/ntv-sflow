# MVP Task Plan

Progress is tracked here; see `IMPLEMENTATION_PLAN.md` for the current plan and milestone notes.

This is the initial implementation backlog for Claude Code.

## Milestone 0 — Repository scaffold

- [x] Initialize Go module
- [x] Initialize Next.js + TypeScript app
- [x] Tailwind setup
- [x] shadcn/ui setup
- [x] lint/format/test commands
- [x] config loading skeleton
- [x] document actual repository structure
- [x] decide OpenAPI/schema workflow

Acceptance:
- frontend starts
- Go tests run
- CI-ready commands documented

## Milestone 1 — Shared model + Mock provider

- [x] Define frontend domain types
- [x] Define API DTOs
- [x] Create deterministic mock traffic engine
- [x] Create shared mock flow model
- [x] Implement default/heavy-download/internal-backup/unknown scenarios
- [x] Mock status endpoint/provider
- [x] Mock WebSocket-like update adapter

Acceptance:
- one underlying mock dataset can drive both views

## Milestone 2 — Globe View MVP

- [x] Add Globe route
- [x] Integrate globe.gl
- [x] Render globe
- [x] Render origin marker
- [x] Render destination markers
- [x] Render traffic arcs
- [x] Render direction animation
- [x] Add grouping selector
- [x] Add direction filter
- [x] Add min traffic/top-N
- [x] Add destination inspector
- [x] Add source device list
- [x] Add "Open in Home Network"
- [x] Reduced-motion handling
- [x] Empty/unknown geo states

Acceptance:
- user can visually inspect destinations and navigate to Home with context

## Milestone 3 — Home View MVP

- [x] Add Home route
- [x] Integrate React Flow
- [x] Device node components
- [x] Traffic edges
- [x] Internet aggregate node
- [x] Traffic/Topology/Hybrid selector
- [x] Basic mock topology
- [x] Device inspector
- [x] Edge inspector
- [x] Device focus
- [x] VLAN/type/min-bps filters
- [x] "Show in Globe"
- [x] preserve cross-view filters
- [x] avoid layout jitter during updates

Acceptance:
- user can inspect internal traffic and navigate to Globe with device context

## Milestone 4 — App shell and state

- [x] Header
- [x] Globe/Home tabs
- [x] live/mock status
- [x] download/upload summary
- [x] collapsible filters
- [x] collapsible inspector
- [x] shared filter store
- [x] URL/shareable query state
- [x] stale state handling

## Milestone 5 — Backend API skeleton

- [x] `/api/v1/status`
- [x] `/api/v1/globe`
- [x] `/api/v1/home/traffic`
- [x] `/api/v1/home/topology`
- [x] `/api/v1/devices`
- [x] `/api/v1/devices/{id}`
- [x] `/api/v1/flows`
- [x] `/api/v1/ws`
- [x] mock backend adapter (Go port with golden parity, D-039)
- [x] API validation/errors

Acceptance:
- frontend can swap local mock provider for HTTP/WebSocket provider

## Milestone 6 — sFlow collector

- [x] UDP listener
- [x] sFlow v5 datagram parser
- [x] flow sample decoder
- [x] generic packet/header fields needed by product
- [x] counter sample decoder
- [x] normalized domain output
- [x] malformed datagram safety
- [x] parser unit tests
- [x] fuzz tests
- [x] collector metrics

Acceptance:
- real exporter can produce normalized flow/counter events

## Milestone 7 — Aggregation

- [x] internal CIDR classifier
- [x] IPv4/IPv6 support
- [x] sampled byte estimates
- [x] 1s/5s windows
- [x] external group aggregation
- [x] internal edge aggregation
- [x] top-N
- [x] thresholds
- [x] observation point semantics
- [x] interface counter rates
- [x] counter reset handling

## Milestone 8 — Enrichment

- [x] GeoIP adapter
- [x] ASN adapter
- [x] device registry
- [x] unknown handling
- [x] enrichment cache
- [x] source-device attribution where available

## Milestone 9 — Persistence

- [ ] PostgreSQL schema/migrations — deferred until something writes inventory/settings (D-060)
- [x] ClickHouse schema (`flow_seconds` + 1 m / 1 h rollups, `counter_rates`)
- [x] repository interfaces (`history.Store`: memory, ClickHouse)
- [x] retention/TTL decision (D-059)
- [x] historical flow query (`/flows?start&end&cursor`)
- [x] aggregate query (historical windows on every view endpoint, `/history/timeline`)
- [x] timeline / replay UI

## Milestone 10 — Production hardening

- [ ] Docker images
- [ ] compose refinement
- [ ] health checks
- [ ] config validation
- [ ] backpressure
- [ ] WebSocket client limits
- [ ] secure defaults
- [ ] deploy docs

## Deferred

- [ ] SNMP discovery
- [ ] LLDP/CDP
- [ ] C9800 integration
- [ ] Kubernetes integration
- [ ] vSphere integration
- [ ] full replay UI
- [ ] alerts
- [ ] authentication
- [ ] multi-user
- [ ] HA collector
