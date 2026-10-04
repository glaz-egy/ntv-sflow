# Data Model

## 1. Principles

1. IP address is not a permanent device identity.
2. Device identity can have multiple IPs over time.
3. A flow record is an observation, not a guaranteed unique end-to-end event.
4. Measurements must identify whether they are exact counters or sampled estimates.
5. IPv4 and IPv6 are first-class.
6. Unknown metadata is represented explicitly.

## 2. Core domain entities

### Device

```text
Device
- id: UUID
- display_name: string
- device_type: enum
- status: enum
- vendor: optional string
- model: optional string
- os: optional string
- first_seen_at
- last_seen_at
- metadata: json
```

Device types:
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
- kubernetes_workload (future)
- internet
- unknown

### DeviceAddress

```text
DeviceAddress
- id
- device_id
- address
- address_family
- valid_from
- valid_to
- source
- confidence
```

Sources:
- static
- dhcp
- arp
- nd
- dns
- wlc
- flow
- manual

### DeviceMac

```text
DeviceMac
- id
- device_id
- mac
- valid_from
- valid_to
- source
```

### Network

```text
Network
- id
- name
- cidr
- address_family
- is_internal
- vlan_id optional
- site_id optional
```

### Exporter

```text
Exporter
- id
- name
- agent_address
- protocol
- enabled
- metadata
```

### Interface

```text
Interface
- id
- exporter_id/device_id
- if_index
- name
- description
- speed_bps
- mac optional
- vlan metadata optional
```

### TopologyLink

```text
TopologyLink
- id
- source_device_id
- source_interface_id optional
- target_device_id
- target_interface_id optional
- link_type
- source
- confidence
- first_seen_at
- last_seen_at
```

link_type:
- physical
- wireless_association
- logical
- unknown

source:
- manual
- lldp
- cdp
- wlc
- inferred

"Inferred" links must be rendered differently from confirmed links.

## 3. Flow observation

Normalized structure:

```text
FlowObservation
- observed_at
- exporter_id
- agent_address
- input_if_index optional
- output_if_index optional

- src_ip
- dst_ip
- address_family
- src_port optional
- dst_port optional
- ip_protocol

- sampled_packet_length_bytes
- sampling_rate
- estimated_bytes

- src_internal: bool
- dst_internal: bool

- src_device_id optional
- dst_device_id optional

- destination_geo_id optional
- destination_asn optional
```

Derived:

```text
estimated_bytes =
  sampled_packet_length_bytes * sampling_rate
```

Do not call this exact bytes.

## 4. Interface counter observation

```text
InterfaceCounterObservation
- observed_at
- exporter_id
- if_index
- if_in_octets
- if_out_octets
- if_in_errors optional
- if_out_errors optional
- if_speed_bps optional
- admin_status optional
- oper_status optional
```

Derived rate:

```text
rx_bps = delta(if_in_octets) * 8 / delta_time
tx_bps = delta(if_out_octets) * 8 / delta_time
```

Counter-reset/wrap cases must be detected.

## 5. Geo destination

```text
GeoDestination
- ip/prefix reference
- country_code
- country_name
- city optional
- latitude optional
- longitude optional
- asn optional
- organization optional
- source_database
- updated_at
```

Geo coordinates are approximate metadata.

## 6. Live aggregate model

### ExternalTrafficAggregate

```text
- window_start
- window_end
- grouping: country|city|asn|ip
- group_key
- display_name
- latitude optional
- longitude optional
- asn optional
- organization optional
- inbound_estimated_bps
- outbound_estimated_bps
- source_device_count
- source_device_ids limited
- sample_count
```

### InternalTrafficAggregate

```text
- window_start
- window_end
- src_device_id / src_endpoint_key
- dst_device_id / dst_endpoint_key
- estimated_bps
- estimated_pps optional
- top_protocols
- observation_points
```

### InterfaceRate

```text
- observed_at
- device_id
- interface_id
- rx_bps
- tx_bps
- measurement_kind: counter
```

## 7. API measurement metadata

Where values can be confused, include:

```json
{
  "value": 125000000,
  "unit": "bps",
  "measurement_kind": "sampled_estimate",
  "window_seconds": 5
}
```

or:

```json
{
  "value": 993000000,
  "unit": "bps",
  "measurement_kind": "counter",
  "interval_seconds": 20
}
```

## 8. PostgreSQL conceptual tables

- devices
- device_addresses
- device_macs
- networks
- exporters
- interfaces
- topology_links
- settings
- sites (future)
- users/roles (future)

## 9. ClickHouse conceptual tables

### flow_observations
Partition/TTL strategy should be decided after measured ingest volume.

Suggested dimensions:
- timestamp
- exporter
- src/dst address
- src/dst port
- protocol
- interfaces
- internal flags
- sampling rate
- estimated bytes
- resolved device IDs
- ASN/country

### traffic_aggregates_1s / 1m / 1h
Materialized or batch aggregates depending on operational simplicity.

## 10. Identity resolution

Resolution order should be configurable.

Example:
1. static/manual mapping
2. authoritative integration (WLC/DHCP)
3. MAC/IP neighbor evidence
4. flow-only IP endpoint
5. unknown

Never silently merge two devices based only on weak evidence.
