# Globe View Specification

## 1. Purpose

Globe View answers:

> Where is external traffic going, and which internal devices are responsible?

The globe is the dominant UI element.

## 2. Required desktop layout

```text
┌────────────────────────────────────────────────────────────┐
│ App        [Globe] [Home]      LIVE / MOCK      Settings  │
├────────────────────────────────────────────────────────────┤
│ ↓ Download     ↑ Upload     Total      Destinations        │
├──────────────┬──────────────────────────┬──────────────────┤
│ Filters      │                          │ Inspector        │
│              │        3D GLOBE          │                  │
│ Grouping     │                          │ Destination      │
│ Direction    │   animated traffic arcs  │ ASN / Country    │
│ Source       │                          │ RX/TX            │
│ Protocol     │                          │ Source devices   │
│ Threshold    │                          │                  │
├──────────────┴──────────────────────────┴──────────────────┤
│ LIVE / PAUSE        timeline / last update                │
└────────────────────────────────────────────────────────────┘
```

Left/right panels are collapsible.

## 3. Globe implementation

Preferred:
- `globe.gl`

Acceptable if justified:
- Three.js
- react-three-fiber

Requirements:
- interactive rotation
- zoom
- responsive
- dark visual style
- smooth updates without recreating the entire scene every tick
- reduced-motion support

## 4. Origin

The origin is a configured coarse location.

Requirements:
- do not require a street address
- support city/region precision
- store/display precision metadata
- origin is a visualization point, not a privacy-sensitive exact physical claim

## 5. Destination grouping

Modes:
1. Country
2. City
3. ASN
4. IP

Default:
- ASN or Country, decided in implementation after UX evaluation
- make default configurable

## 6. Destination markers

Visual encoding:
- position: GeoIP approximate coordinate
- radius: relative traffic magnitude
- pulse: optional activity
- selected state: persistent highlight

When no coordinate:
- do not place random/zero coordinate marker
- surface in "Unknown location" grouping/panel

## 7. Traffic arcs

Arc semantics:
- visual relation between origin and GeoIP destination
- NOT physical route

Encoding:
- width: traffic magnitude (bounded/log-like scaling recommended)
- direction: particles/animated dash
- inbound/outbound visually distinguishable
- transparency: de-emphasize lower traffic
- selected arc remains legible

Avoid:
- thousands of simultaneous arcs
- linear width that lets a single huge flow dominate the entire globe

## 8. Traffic particles

Particles are illustrative aggregate animation.

They must not imply:
- one particle = one packet
- packet-level timing

User setting:
- particles on/off

Reduced motion:
- disable continuous animation where preferred
- preserve static direction indicators

## 9. Inspector

When a marker/arc is selected, show:
- label
- IP/prefix when applicable
- country
- city if available
- ASN
- organization
- inbound bps
- outbound bps
- measurement kind
- top internal source devices
- top protocols/ports
- last seen
- observation/exporter information where useful

Action:
- **Open in Home Network**

The action preserves:
- destination selection/filter
- time
- source filters
- protocol
where semantically valid.

## 10. Filters

Primary visible filters:
- grouping
- direction
- source device
- protocol
- minimum traffic
- top N

Advanced:
- destination country
- ASN
- IP
- VLAN
- exporter
- IPv4/IPv6

## 11. Summary metrics

Supporting only:
- Download
- Upload
- Total
- visible destinations

Prefer boundary-interface counters for total WAN throughput if configured.

Clearly mark sampled estimate when not counter-derived.

## 12. State transitions

### Destination selected
- persist selection
- dim unrelated arcs
- populate inspector

### Device filter selected
- show only arcs attributable to device
- UI chip indicates active source

### View switched to Home
- carry destination filter
- Home highlights relevant internal sources

## 13. Loading and empty states

Loading:
- globe remains rendered
- skeleton/quiet loading state for data

No traffic:
- do not invent arcs
- show "No matching traffic in this window"

Geo unavailable:
- list unknown destinations in inspector/secondary panel

## 14. Performance

Initial target:
- <= ~100 visible arcs
- update data in place
- aggregate before rendering
- cap particle count independently from flow count

## 15. Accessibility

- keyboard-accessible filters and inspector
- selection details not available only via hover
- visible non-color direction labels/icons
- reduced-motion support
- high contrast text
