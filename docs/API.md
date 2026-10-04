# API Specification

Base path:

```text
/api/v1
```

**The machine-readable contract is `api/openapi.yaml` (source of truth, D-021).** This document explains it. When the two disagree, the YAML wins.

Implemented by `cmd/api` (Go). `status.mode` is `mock` (deterministic mock engine) or `live` (sFlow collector data, D-053). In live mode, `collector.status` is `disconnected` before the first datagram, `connected` while data arrives, and `stale` after `live.stale_after` without datagrams. `mock` is null. Prometheus metrics are served at `GET /metrics` in live mode and whenever history is enabled. Run it with `pnpm dev:api`, or see `docs/SETUP.ja.md`.

## 1. General response conventions

Timestamps:
- RFC3339 UTC in API
- frontend localizes for display

IP addresses:
- strings
- support IPv4 and IPv6

Large integer values:
- JSON numbers are acceptable while safe in language/runtime
- otherwise use string/typed serialization consistently

Rates (D-023):
- every bps value is a `Measurement`:
  `{ "value": 125000000, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5, "sample_count": 412 }`
  or `{ "value": 993000000, "unit": "bps", "measurement_kind": "counter", "interval_seconds": 5 }`

Destination keys:
- `<grouping>:<value>` — `asn:13335`, `country:JP`, `city:JP:Tokyo`, `ip:198.51.100.10`, `ip:2001:db8::1`
- unknown groups: `asn:unknown`, `country:unknown`, `city:unknown`
- parse by the first colon only

Node ids (D-027):
- devices: opaque persistent id; unresolved endpoints: `ep:<ip>`; Internet aggregate: `internet`; split-out destination: `dst:<destination key>`

TypeScript types are generated from `api/openapi.yaml` into `apps/web/src/contracts/generated/` (D-021).

Historical windows (D-059):
- `start` / `end` (RFC3339; `[start, end)`) on `/globe`, `/globe/destinations/{key}`, `/home/traffic`, `/home/topology`, `/devices`, `/devices/{id}` and `/flows` answer for that range from history. Omit both for the live window.
- The range is aligned outwards to the stored resolution and trimmed to the stored data. The response `window` is the range actually used. Rates are averages over it (sampled estimates; WAN counters when they cover ≥ 80 %).
- Errors: `400 INVALID_FILTER` (malformed, or only one of the two), `400 INVALID_RANGE` (end ≤ start, end in the future, longer than 400 d), `501 HISTORY_UNAVAILABLE` (`history.backend: none`), `503 HISTORY_ERROR` (store unreachable).

## 2. Status

### GET /status

Response:

```json
{
  "mode": "mock",
  "live": true,
  "collector": {
    "status": "connected",
    "last_datagram_at": "2026-10-03T14:45:30Z"
  },
  "last_aggregate_at": "2026-10-03T14:45:30Z",
  "server_time": "2026-10-03T14:45:32Z",
  "update_interval_seconds": 1,
  "window_seconds": 5,
  "mock": { "seed": 42, "scenario": "default", "speed": 1 },
  "history": {
    "backend": "clickhouse",
    "earliest": "2026-09-28T00:00:00Z",
    "latest": "2026-10-03T14:45:25Z",
    "tiers": [
      { "step_seconds": 1, "retention_seconds": 604800 },
      { "step_seconds": 60, "retention_seconds": 7776000 },
      { "step_seconds": 3600, "retention_seconds": 31536000 }
    ]
  }
}
```

## 3. Globe snapshot

### GET /globe

Query:
- grouping=country|city|asn|ip
- source_node_id
- direction=inbound|outbound|both (ranking/min_bps use the selected direction)
- protocol
- min_bps
- limit
- start/end: historical window (see §1)

Response:

```json
{
  "window": {
    "start": "2026-10-03T14:45:25Z",
    "end": "2026-10-03T14:45:30Z"
  },
  "origin": {
    "label": "Home Network",
    "latitude": 35.68,
    "longitude": 139.76,
    "precision": "city"
  },
  "grouping": "asn",
  "destinations": [
    {
      "key": "asn:13335",
      "grouping": "asn",
      "label": "Cloudflare",
      "ip": null,
      "country_code": null,
      "country_name": null,
      "city": null,
      "location": { "latitude": 35.68, "longitude": 139.69, "basis": "asn_dominant" },
      "asn": 13335,
      "organization": "Cloudflare",
      "outbound_bps": { "value": 9100000, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5, "sample_count": 118 },
      "inbound_bps": { "value": 78000000, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5, "sample_count": 361 },
      "source_device_count": 3,
      "source_node_ids": ["dev_phone01", "dev_pc01", "dev_laptop01"],
      "last_seen": "2026-10-03T14:45:30Z"
    }
  ],
  "truncated_count": 0,
  "summary": {
    "download": { "value": 214000000, "unit": "bps", "measurement_kind": "counter", "interval_seconds": 5 },
    "upload": { "value": 28600000, "unit": "bps", "measurement_kind": "counter", "interval_seconds": 5 },
    "basis": "boundary_counter"
  }
}
```

`country_code`/`city` are null when unknown **or** when a group spans several values; `members` in the inspector response disambiguates. `location.basis` explains the marker position (D-022); `location: null` means unknown and is never replaced by a placeholder.

Geo coordinates above are illustrative mock data only.

## 4. Globe destination inspector

### GET /globe/destinations/{key}

Query:
- source_node_id optional
- protocol optional

Response (`GlobeDestinationDetail`):
- `destination` (same shape as in `/globe`)
- `top_sources[]`: node_id, label, device_type, resolved, inbound_bps, outbound_bps
- `top_protocols[]`: protocol, port, service, bps
- `members[]`: individual IPs behind a grouped destination
- `observation_points[]`: exporter_id, exporter_name, input/output ifIndex, sampling_rate, used_for_aggregate

404 when no matching traffic exists in the current window.

## 5. Globe ranking endpoints

Optional if `/globe` already supplies sufficient ranked data:

- GET `/globe/countries`
- GET `/globe/asns`
- GET `/globe/destinations`

## 6. Home snapshot

### GET /home/traffic

Query:
- focus_node_id (keep only edges touching this node)
- destination_key (split matching external traffic into a `dst:<key>` node and report its sources)
- vlan_id
- device_types (comma-separated)
- protocol
- min_bps (on edge total)
- limit (edges, top N by total)
- include_inactive=false
- scope=internal|external|both

Response:

```json
{
  "window": { "start": "2026-10-03T14:45:25Z", "end": "2026-10-03T14:45:30Z" },
  "server_time": "2026-10-03T14:45:30Z",
  "nodes": [
    {
      "id": "dev_pc01",
      "kind": "device",
      "label": "pc-01",
      "device_type": "pc",
      "status": "online",
      "addresses": ["10.20.0.10", "fd00:20::10"],
      "vlan_id": 20,
      "network_name": "clients",
      "rx_bps": { "value": 120000000, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5, "sample_count": 540 },
      "tx_bps": { "value": 450000000, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5, "sample_count": 4100 },
      "last_seen": "2026-10-03T14:45:30Z"
    }
  ],
  "edges": [
    {
      "id": "e:dev_nas01~dev_pc01",
      "source": "dev_nas01",
      "target": "dev_pc01",
      "scope": "internal",
      "forward_bps": { "value": 19000000, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5, "sample_count": 90 },
      "reverse_bps": { "value": 445000000, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5, "sample_count": 3900 },
      "top_protocols": [{ "protocol": "tcp", "port": 445, "service": "smb", "bps": { "value": 440000000, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5 } }],
      "observation_points": [{ "exporter_id": "exp_core", "exporter_name": "core-switch", "input_if_index": 6, "output_if_index": 3, "used_for_aggregate": true, "sampling_rate": 512 }],
      "last_seen": "2026-10-03T14:45:30Z"
    }
  ],
  "truncated_count": 0,
  "destination_context": null,
  "summary": {
    "wan_total": { "value": 243000000, "unit": "bps", "measurement_kind": "counter", "interval_seconds": 5 },
    "internal_estimated": { "value": 640000000, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5 },
    "device_count": 13,
    "online_count": 13
  }
}
```

Edges are logical relationships between an endpoint pair (`source`/`target` ordered canonically). `forward_bps` is source→target and `reverse_bps` is target→source. External traffic is collapsed into the `internet` node unless it matches `destination_key` (D-032).

## 7. Topology

### GET /home/topology

Response:

```json
{
  "nodes": [],
  "links": [],
  "generated_at": "2026-10-03T14:45:00Z"
}
```

Every topology link carries:
- `source_node_id`, `target_node_id`
- `link_type`: physical | wireless_association | logical | unknown
- `evidence`: manual | lldp | cdp | wlc | inferred (`inferred` must render differently)
- `confidence`: 0..1
- `source_interface`, `target_interface` (nullable)

Nodes use the same `HomeNode` shape as `/home/traffic`. Unresolved endpoints are never given fabricated links.

## 8. Device list

### GET /devices

Filters:
- type
- status
- vlan
- search

## 9. Device inspector

### GET /devices/{id}

Query:
- grouping (for `top_external_destinations` keys; default asn)

Response (`DeviceDetail`):
- `node` (HomeNode: identity, type, status, addresses, VLAN, rx/tx, last_seen)
- `addresses[]` with family and source (dhcp/static/nd/flow/...)
- `macs[]` (empty when unknown)
- `ssid`, `vendor`, `model` (null when unknown)
- `attachment`: via_node_id, via_label, interface, evidence — null when topology is unknown
- `top_internal_peers[]`: node_id, label, device_type, tx_bps, rx_bps
- `top_external_destinations[]`: key, label, country_code, outbound_bps, inbound_bps

Unknown fields are null (or empty lists), never guessed. Accepts `ep:<ip>` ids for unresolved endpoints seen in the window.

## 10. Flow search

### GET /flows

Filters:
- start
- end
- source
- destination
- source_device_id
- destination_device_id
- protocol
- src_port
- dst_port
- exporter
- input_if
- output_if
- internal_scope
- limit
- cursor

Also: `exporter`, `internal_scope=internal|external|transit`, `limit` (≤ 1000). Each result is a `FlowRecord`: endpoints, ports, scope, direction, resolved node ids, a sampled-estimate `bps`, and the exporter whose observation was used.

This endpoint is for investigation, not the main visualization transport. It searches the live window, or `[start, end)` from history. Results are ordered by estimated bytes. `truncated_count` counts the matches after this page, and `next_cursor` (opaque, null on the last page) is passed as `cursor` for the next one.

## 10a. History timeline

### GET /history/timeline

Query: `start`, `end` (required; an `end` in the future is clamped to now), `max_points` (2–720, default 720).

```json
{
  "window": { "start": "2026-10-03T13:45:00Z", "end": "2026-10-03T14:45:00Z" },
  "step_seconds": 5,
  "points": [
    {
      "start": "2026-10-03T13:45:00Z",
      "has_data": true,
      "inbound":  { "value": 2.1e8, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5, "sample_count": 795 },
      "outbound": { "value": 2.9e7, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5, "sample_count": 238 },
      "internal": { "value": 5.4e8, "unit": "bps", "measurement_kind": "sampled_estimate", "window_seconds": 5, "sample_count": 2051 },
      "wan_download": { "value": 2.2e8, "unit": "bps", "measurement_kind": "counter", "interval_seconds": 5 },
      "wan_upload": { "value": 3.1e7, "unit": "bps", "measurement_kind": "counter", "interval_seconds": 5 }
    }
  ]
}
```

`inbound` is external → internal (download), and `outbound` is internal → external (upload). Each flow key is counted once per bucket, from the preferred observation point (D-024). `wan_*` are boundary counter rates and are null without counters. Buckets without stored data have `has_data: false` and null rates. A bucket at the edge of the stored data is averaged over its covered seconds (`window_seconds` says how many).

## 11. WebSocket

### WS /api/v1/ws

Client subscribe message:

```json
{
  "type": "subscribe",
  "channels": ["globe", "home", "status"],
  "filters": {
    "source_node_id": null,
    "min_bps": 1000000
  }
}
```

Server event envelope:

```json
{
  "type": "globe_update",
  "sequence": 1012,
  "server_time": "2026-10-03T14:45:32Z",
  "payload": {}
}
```

Event types:
- `window_update` (window end + status, once per aggregate window; D-031)
- `globe_update`
- `home_update`
- `device_update`
- `collector_status`
- `heartbeat`
- `error`

## 12. WebSocket design rule

Do not emit one WebSocket event per raw sFlow sample.

Emit bounded aggregate snapshots/deltas.

## 13. Error format

```json
{
  "error": {
    "code": "INVALID_FILTER",
    "message": "min_bps must be >= 0"
  }
}
```

## 14. Future configuration API

Future endpoints:
- `/settings/networks`
- `/settings/exporters`
- `/settings/boundaries`
- `/settings/visualization`

Do not expose unsafe collector configuration without authentication once remote access is introduced.
