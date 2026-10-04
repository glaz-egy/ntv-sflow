# Product Specification

## 1. Product summary

Network Traffic Visualizer is a visualization-first web application for understanding network communication.

It has two primary modes:

### Globe View
Visualizes external destinations on a 3D globe.

Primary question:

> Where in the world is traffic going?

### Home Network View
Visualizes internal devices and communication relationships.

Primary question:

> Which internal device is communicating with whom?

The views are connected so users can move from an external destination to its internal source devices and from an internal device to its external destinations.

## 2. Product goals

### G1 — External traffic visibility
Make external destinations understandable using geography, ASN and traffic magnitude.

### G2 — Internal device visibility
Make device-to-device and device-to-internet communication understandable without reading raw flow tables.

### G3 — Investigation continuity
Preserve filters and selected context when switching between Globe and Home views.

### G4 — Real-time-first
Show live traffic with a refresh cadence appropriate to aggregated sFlow data.

### G5 — Correct uncertainty
Clearly distinguish:
- exact counter-derived values
- sampled estimates
- inferred metadata
- unknown metadata

## 3. Non-goals

The MVP is not intended to be:
- a full SIEM
- a packet capture/PCAP replacement
- a generic Grafana replacement
- a complete NMS
- a full NetBox replacement
- a complete topology discovery platform
- a forensic packet replay system

## 4. Primary users

Initial user:
- network administrator/operator of a home lab or small infrastructure environment
- comfortable with VLANs, routing, sFlow, SNMP, BGP and Kubernetes
- values intuitive visualization over raw CLI output

The data model should not assume only one physical home network because the project may later expand.

## 5. Core user journeys

### Journey A — External destination to internal source
1. Open Globe View.
2. Observe high-volume arc to an ASN/country/IP.
3. Select the arc/destination.
4. Inspect destination details.
5. See internal source devices.
6. Click "Open in Home Network".
7. Home View highlights those devices and related traffic.

### Journey B — Internal device to external destinations
1. Open Home Network View.
2. Select a PC/server/phone.
3. Inspect top peers.
4. Click "Show external destinations".
5. Globe View opens with the device as the active source filter.
6. Only relevant external arcs remain emphasized.

### Journey C — Internal communication
1. Open Home View in Traffic mode.
2. Select a device.
3. View internal peers and estimated rates.
4. Focus on one edge.
5. Inspect protocol/ports/observation details.

### Journey D — Topology context
1. Switch Home View to Topology or Hybrid.
2. Review known router/switch/AP/device relationships.
3. Keep traffic overlays separate from unknown physical paths.

## 6. Common UX

Desktop layout:
- Header
- Collapsible left filter panel
- Main visualization
- Collapsible right inspector
- Bottom timeline/status area

Common state:
- data source mode: Mock / Live
- live state: Live / Paused
- selected time/range
- source filters
- destination filters
- protocol
- VLAN
- minimum traffic threshold
- aggregation level

## 7. Live state

Default:
- live mode enabled
- visualization update target: 1–5 seconds
- exact cadence configurable

The app must not animate at a granularity that implies packet-level precision.

Every live payload should include:
- server timestamp
- window start
- window end
- freshness/staleness metadata where appropriate

## 8. History

MVP:
- live-first
- architecture must retain timestamped aggregate models

Later:
- Pause
- timeline scrub
- historical snapshot
- replay at 0.5x / 1x / 2x / 4x

## 9. Filters

Common:
- source IP/device
- destination IP
- internal/external
- protocol
- port
- VLAN
- exporter
- address family
- minimum bps
- top N

Globe:
- Country
- City
- ASN
- IP
- inbound/outbound

Home:
- node type
- VLAN
- SSID
- active only
- internal only
- external only
- selected-device focus

## 10. Success criteria

MVP is successful when a user can answer, visually and within seconds:

- What are the largest external destinations right now?
- Which internal devices are generating that traffic?
- What are the largest internal device-to-device relationships?
- Is a value counter-measured or sampled-estimated?
- Which observation point produced the evidence?
