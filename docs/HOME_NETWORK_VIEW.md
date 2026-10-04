# Home Network View Specification

## 1. Purpose

Home Network View answers:

> Which internal devices are communicating, and which devices are responsible for external traffic?

It has three display modes:
- Traffic
- Topology
- Hybrid

Default:
- Traffic

## 2. Desktop layout

```text
┌─────────────────────────────────────────────────────────────┐
│ App          [Globe] [Home]     LIVE / MOCK      Settings │
├─────────────────────────────────────────────────────────────┤
│ Total       Internal       Devices       Online             │
├─────────────┬──────────────────────────────┬────────────────┤
│ Filters     │                              │ Device/Flow    │
│             │      NETWORK CANVAS          │ Inspector      │
│ [Traffic]   │                              │                │
│ [Topology]  │ nodes + traffic edges        │ identity       │
│ [Hybrid]    │                              │ addresses       │
│ VLAN        │                              │ peers          │
│ Type        │                              │ destinations   │
│ Threshold   │                              │                │
├─────────────┴──────────────────────────────┴────────────────┤
│ LIVE / PAUSE        timeline / last update                 │
└─────────────────────────────────────────────────────────────┘
```

## 3. Rendering library

Preferred:
- React Flow

Alternatives if scale demands:
- Cytoscape.js
- Sigma.js

Do not change solely for preference; document performance reasons.

## 4. Node types

Required:
- internet
- router
- firewall
- switch
- wireless_ap
- server
- pc
- smartphone
- iot
- vm
- kubernetes_node
- unknown

Future:
- pod
- service
- container

## 5. Node identity

Persistent key:
- internal device UUID

Do not use:
- IP address
- hostname alone
as permanent identity.

Unknown endpoints may use a temporary endpoint identity until resolved.

## 6. Node content

Standard node may show:
- icon/type
- display name
- primary IP
- online/stale state
- RX
- TX

Compact mode:
- label
- activity

Grouped node:
- group label
- number of devices
- aggregate traffic

## 7. Traffic mode

Shows communication relationships independent of physical route.

Example:
```text
PC-01 ─── 720 Mbps ─── NAS
PC-01 ─── 100 Mbps ─── Internet
```

Edge:
- source/target = communicating endpoints
- width = estimated traffic magnitude
- direction animation/arrow
- internal/external visual distinction

This does not claim physical path.

## 8. Topology mode

Shows known physical/logical structure.

Examples:
- Router → Core Switch
- Core Switch → AP
- AP → Wi-Fi client
- Switch port → Server

Every topology link carries source/confidence.

Styles should distinguish:
- confirmed/manual
- LLDP/CDP
- WLC association
- inferred/uncertain

## 9. Hybrid mode

Topology is the structural layer.
Traffic is overlaid.

Rules:
- only map traffic to a physical path when actual path evidence exists
- otherwise draw logical communication edge independently
- never visually imply unsupported routing path

## 10. Internet node

External destinations may be collapsed into:
- Internet
- ASN group
- selected external destination
depending on focus mode.

Avoid exploding every external IP into a Home graph node by default.

## 11. Filters

Primary:
- view mode
- VLAN
- node type
- minimum bps
- top N
- active only
- internal/external/both

Advanced:
- SSID
- selected exporter
- protocol
- IPv4/IPv6
- device
- destination ASN

## 12. Device inspector

Show when device selected:
- display name
- type
- IP addresses
- MACs
- VLAN
- SSID
- connected AP
- connected switch/interface
- RX/TX
- measurement source
- last seen
- top internal peers
- top external destinations

Actions:
- Show external destinations in Globe
- Focus device
- Clear focus

## 13. Flow/edge inspector

Show:
- source
- destination
- estimated bps
- measurement kind
- protocol breakdown
- ports where meaningful
- observation points
- last seen
- sampling metadata if relevant

## 14. Cross-view behavior

From Home to Globe:
- selected internal device becomes source filter
- external traffic destinations are emphasized
- time state persists

From Globe to Home:
- selected external destination becomes destination filter
- relevant internal source nodes are emphasized
- unrelated traffic is dimmed/hidden according to UI mode

## 15. Layout

MVP:
- automatic layout
- deterministic enough not to jump every live update
- preserve node positions while traffic values change

Later:
- manual position save
- layout profiles
- grouping/containers by VLAN/SSID/site

## 16. Edge scaling

Use bounded nonlinear scaling for readability.

Example conceptual mapping:
- < 1 Mbps: hidden by default or very thin
- 1–10 Mbps: thin
- 10–100 Mbps: medium
- 100 Mbps–1 Gbps: thick
- >1 Gbps: capped thick

Actual thresholds configurable.

## 17. Performance

Target:
- 100–300 visible nodes
- bounded edges
- no full relayout every second
- filter before rendering
- aggregate external endpoints

## 18. Empty/unknown states

Unknown device:
- generic node
- IP label
- "Unresolved device"

Unknown topology:
- traffic still displayed
- topology not fabricated

Inactive:
- optional display
- visually subdued
