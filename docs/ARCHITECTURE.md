# Architecture

## 1. Overview

The system is divided into five logical layers:

```text
[ Exporters ]
     │
     ▼
[ Collector ]
     │ normalized observations
     ▼
[ Aggregation + Enrichment ]
     │
     ├── live aggregates
     ├── metadata joins
     └── persistence
     │
     ▼
[ API + WebSocket ]
     │
     ▼
[ Web Visualization ]
```

The visualization layer must never depend directly on raw sFlow structures.

## 2. Components

### 2.1 Collector
Responsibilities:
- listen for UDP sFlow v5 datagrams
- decode datagram/sample/record structures
- convert records into normalized domain events
- retain exporter and observation-point identity
- avoid heavy geo/device lookups on the UDP receive hot path where possible

Outputs:
- normalized flow observations
- normalized interface counter observations
- collector health metrics

### 2.2 Aggregation
Responsibilities:
- 1s/5s/other configurable windows
- sampled-byte estimation
- device-to-device grouping
- device-to-external grouping
- ASN/country/city/IP grouping
- top-N calculation
- minimum-threshold filtering
- preserve observation context

### 2.3 Enrichment
Independent enrichment modules:
- internal CIDR classification
- GeoIP
- ASN
- device registry
- DNS/DHCP/ARP later
- LLDP/CDP later
- WLC later
- Kubernetes/vSphere later

An enrichment failure must not drop the flow.

### 2.4 Metadata store
PostgreSQL:
- devices
- device identities
- networks
- interfaces
- configured exporters
- topology links
- visualization preferences
- enrichment metadata

### 2.5 Flow/history store
ClickHouse:
- normalized flow observations (optional raw retention policy)
- aggregate time buckets
- external destination aggregates
- internal communication aggregates

### 2.6 Live cache
Redis is optional for MVP.

Use it when:
- multiple API replicas need a shared latest-state cache
- live ranking computation benefits materially
- pub/sub simplifies distribution

Do not require Redis merely because it appears in the target architecture.

### 2.7 API
REST:
- initial snapshots
- metadata
- historical queries
- inspectors
- configuration

WebSocket:
- incremental live aggregates
- status changes

### 2.8 Web frontend
Next.js application.

Major modules:
- shell/layout
- shared filter state
- realtime client
- Globe View
- Home Network View
- inspectors
- settings
- mock/live data provider

## 3. Domain separation

Recommended internal packages:

```text
internal/
  flow/          # normalized flow domain
  sflow/         # protocol decoding
  counters/      # counter domain and math
  aggregation/   # windows and rankings
  enrichment/    # geo/asn/internal/device
  devices/       # registry and identities
  topology/      # known physical/logical topology
  realtime/      # subscriptions/live snapshots
  api/           # REST handlers/contracts
  storage/       # pg/clickhouse repositories
```

Rules:
- `sflow` may depend on parsing utilities, not UI models.
- `flow` contains stable domain concepts.
- API DTOs should not leak protocol-specific structs.
- UI should consume API/domain concepts, not collector structs.

## 4. Data paths

### 4.1 Flow sample path

```text
UDP datagram
→ sFlow decoder
→ FlowObservation
→ classifier/enrichment
→ aggregation windows
→ ClickHouse
→ live broadcaster
→ WebSocket
→ visualization
```

### 4.2 Counter path

```text
UDP datagram
→ sFlow decoder
→ InterfaceCounterObservation
→ delta/rate calculator
→ interface utilization state
→ API/WebSocket
→ summary and node/link metrics
```

## 5. Observation semantics

A flow can be seen by more than one exporter.

Therefore every observation must include:
- exporter ID
- agent address
- input interface
- output interface
- observation timestamp

The system must distinguish:
- "traffic was observed here"
from:
- "this is unique end-to-end traffic"

Global totals should use explicitly configured boundary/interface counters where possible.

## 6. Scale assumptions for MVP

Target interactive UI:
- Globe: ~100 simultaneous visible arcs
- Home: ~100–300 visible nodes
- Home: bounded visible edges via top-N/threshold
- aggregation cadence: 1–5s
- raw arrival rate may be much higher than UI update rate

## 7. Failure handling

### Collector unavailable
UI:
- retain last snapshot if configured
- mark stale
- show last received timestamp

### GeoIP unavailable
- still show destination
- use "Unknown location"
- do not invent coordinates

### Device resolution unavailable
- represent endpoint as unknown IP node
- allow later merge into device identity

### Topology unknown
- Traffic mode remains functional
- Hybrid/Topology should represent unknown/unresolved links explicitly

## 8. Deployment

MVP may run:
- frontend
- API
- collector
- PostgreSQL
- ClickHouse
on one host.

The code must allow:
- collector separation
- API horizontal scaling
- database separation
later.

## 9. Configuration

Runtime config should include:
- collector listen address/port
- aggregation window
- internal CIDRs
- boundary interfaces
- top-N defaults
- GeoIP database paths
- database DSNs
- mock/live mode
- WebSocket limits
