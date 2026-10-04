# CLAUDE.md

## Project
Working title: **Network Traffic Visualizer**

This repository implements a network-flow visualization application centered on two primary experiences:

1. **Globe View** — visualize external destinations on an interactive 3D globe.
2. **Home Network View** — visualize internal devices and their traffic relationships as an interactive graph.

The product is **not** a generic Grafana-like monitoring dashboard. Charts, tables, and rankings are supporting UI. The main visualizations must remain the primary focus.

Read these documents before making architectural or product decisions:

- `docs/PRODUCT_SPEC.md`
- `docs/ARCHITECTURE.md`
- `docs/DATA_MODEL.md`
- `docs/SFLOW.md`
- `docs/API.md`
- `docs/GLOBE_VIEW.md`
- `docs/HOME_NETWORK_VIEW.md`
- `docs/DEVELOPMENT.md`
- `docs/DECISIONS.md`
- `docs/MOCK_DATA.md`
- `docs/TASKS.md`

## Core product principle

> Show **where traffic goes outside the home/network** and **which internal device is responsible for it**.

The two views must be connected. A destination selected in Globe View should be traceable back to internal devices. A device selected in Home Network View should be explorable on the globe.

## Non-negotiable rules

1. Do not replace Globe View with a 2D map.
2. Do not replace Home Network View with a table.
3. Do not make charts the main UI.
4. Do not forward every raw sFlow sample directly to browsers.
5. Do not present sampled flow estimates as exact measurements.
6. Do not treat GeoIP coordinates as the actual packet path.
7. Do not sum samples from multiple observation points as if they were unique traffic.
8. Do not use IP addresses as permanent device IDs.
9. Do not assume sFlow alone reveals complete physical topology.
10. Do not infer a physical path unless topology data actually supports it.
11. Internal networks must be configurable CIDRs, not hard-coded RFC1918 only.
12. IPv6-capable data structures are required even if the first demo primarily uses IPv4.
13. Mock mode must remain usable without a collector.
14. Keep frontend visualization contracts independent from the collector implementation.
15. Important assumptions must be written down in `docs/DECISIONS.md` or the relevant design document.

## Development order

Follow this order unless there is a strong technical reason not to:

1. Repository/architecture scaffolding.
2. Shared TypeScript/OpenAPI contracts or equivalent schema definitions.
3. Mock data provider.
4. Globe View using mock data.
5. Home Network View using mock data.
6. Backend REST/WebSocket API using mock/live adapters.
7. sFlow v5 collector.
8. Aggregation and enrichment.
9. Persistent history.
10. Discovery/enrichment integrations.

Do not block frontend work on the live collector.

## Preferred stack

### Frontend
- Next.js
- TypeScript
- Tailwind CSS
- shadcn/ui
- `globe.gl` as the first implementation choice for Globe View
- React Flow as the first implementation choice for Home Network View

### Backend
- Go
- REST API
- WebSocket for live aggregate updates

### Storage
- PostgreSQL: metadata/configuration/devices/topology
- ClickHouse: flows and time-series aggregates
- Redis: optional live cache; avoid making MVP dependent on Redis if unnecessary

## Repository intent

Suggested layout:

```text
apps/
  web/
cmd/
  collector/
  api/
internal/
  flow/
  aggregation/
  enrichment/
  devices/
  topology/
  storage/
  realtime/
configs/
deploy/
docs/
```

The collector and API may run on the same machine in MVP but must remain logically separable.

## UX requirements

### Common
- Dark theme first.
- Visualization area gets most screen space.
- Left filters and right inspector are collapsible.
- Desktop-first, but responsive.
- Clearly show whether data is live, paused, mock, or stale.
- Filters and selected context should survive view switching.
- Prefer URLs/query state that can be shared/bookmarked.

### Globe View
Must support:
- 3D globe rotation and zoom.
- Destination markers.
- Traffic arcs.
- Directional animation/particles.
- Country / City / ASN / IP aggregation modes.
- Destination inspector.
- Switch to Home Network View while preserving destination filters.

### Home Network View
Must support:
- Traffic / Topology / Hybrid modes.
- Device nodes.
- Internal/external traffic edges.
- Edge widths based on traffic magnitude.
- Device inspector.
- Focus on selected device.
- Switch to Globe View while preserving source-device filters.

## Data correctness

### Exact-ish measurements
Interface utilization should prefer counter samples:
`bps = delta_octets * 8 / delta_time`

### Sampled estimates
Per-flow estimates:
`estimated_bytes = sampled_packet_length * sampling_rate`
`estimated_bps = sum(estimated_bytes) * 8 / aggregation_window_seconds`

Expose that distinction in data contracts where practical, e.g.:
- `measurement_kind: counter`
- `measurement_kind: sampled_estimate`

## Testing expectations

Add tests for:
- CIDR internal/external classification.
- IPv4 and IPv6 parsing.
- sFlow sampling math.
- counter wrap/reset handling.
- aggregation windows.
- deduplication/observation semantics.
- API filters.
- frontend state preservation between Globe/Home.
- mock provider deterministic scenarios.

## Quality requirements

- Prefer small, composable packages.
- No giant all-purpose collector package.
- Keep transport models separate from domain models.
- Avoid frontend coupling to raw sFlow structures.
- Treat geo, ASN, topology and device metadata as enrichment layers.
- Never fabricate unavailable metadata.
- Unknown must remain unknown.
- Graceful degradation is preferable to incorrect certainty.

## Definition of done for the initial MVP

A developer can:
1. Start the application in mock mode.
2. Open Globe View and see animated external traffic.
3. Select a destination and inspect its traffic and internal sources.
4. Switch to Home Network View and see the corresponding source devices highlighted.
5. Select an internal device and inspect internal/external peers.
6. Switch back to Globe View filtered to that device.
7. Run the collector and ingest at least basic sFlow v5 flow/counter samples.
8. Replace mock live updates with aggregated collector data without rewriting the visualization layer.
