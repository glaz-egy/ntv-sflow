# Mock Data Specification

## 1. Purpose

Mock mode allows full UI development without:
- sFlow-capable devices
- databases
- GeoIP files
- collector

Mock behavior must be deterministic enough for tests but dynamic enough to demonstrate live visualization.

## 2. Example device inventory

### Infrastructure
- home-router
- core-switch
- access-switch-01
- ap-main

### Servers
- nas-01
- k8s-node-01
- media-server

### Clients
- pc-01
- laptop-01
- phone-01
- tablet-01

## 3. Example networks

- `10.10.0.0/24` servers
- `10.20.0.0/24` clients
- `10.30.0.0/24` IoT
- `10.40.0.0/24` wireless

These are mock-only values.

## 4. External destination groups

Use fictional/test-safe IPs where individual IPs are displayed.

For organization-level mock labels, examples may include:
- Cloudflare
- Google
- Amazon
- Microsoft
- Discord

Do not embed private user traffic history.

## 5. Mock Globe scenario

At any moment:
- 5–15 active destination groups
- changing traffic magnitude
- mix of inbound/outbound
- at least one unknown-geo destination
- at least one ASN with multiple internal source devices

## 6. Mock Home scenario

Include:
- PC → NAS high internal traffic
- phone → external moderate traffic
- media server → external burst
- IoT → small periodic external traffic
- AP associations
- known topology links

## 7. Deterministic seed

Mock provider should accept:
- seed
- scenario
- speed

Example:

```text
MOCK_SEED=42
MOCK_SCENARIO=default
MOCK_SPEED=1.0
```

## 8. Scenarios

### default
Balanced traffic.

### heavy-download
One client/server receives large external traffic.

### internal-backup
PC/server → NAS dominates internal view.

### many-destinations
Tests Globe aggregation and top-N.

### unknown-metadata
Tests missing device names/geo/topology.

### stale-collector
Tests stale status UX.

## 9. Time model

Mock snapshots:
- update every 1 second by default
- use aggregate windows of 5 seconds
- keep rates smooth enough to visualize

## 10. Cross-view consistency

Critical:
The same mock underlying flows must feed both Globe and Home.

If Globe shows:
- source `pc-01`
- destination ASN X
- 100 Mbps

Then Home filtered to that destination should be able to highlight `pc-01`.

Do not generate unrelated datasets independently per view.
