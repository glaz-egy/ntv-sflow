# sFlow Design

## 1. Scope

MVP collector target:
- sFlow v5
- UDP listener
- flow samples
- counter samples

NetFlow/IPFIX support may be added later behind the same normalized domain model.

## 2. Default port

Default sFlow collector port:
- UDP 6343

Must be configurable.

## 3. Collector pipeline

```text
UDP Receive
→ Datagram Decode
→ Sample Decode
→ Record Decode
→ Normalize
→ Publish to Aggregation
```

Heavy work such as GeoIP lookups should not block UDP receive loops.

## 4. Required flow fields

Normalize where present:
- agent address
- exporter identity
- datagram sequence
- sample sequence
- input interface
- output interface
- source IP
- destination IP
- source port
- destination port
- L4 protocol
- sampled packet length
- sampling rate
- timestamp

Fields may be absent. The decoder must tolerate optional/unknown records.

## 5. Counter fields

At minimum support useful generic interface counters:
- ifIndex
- ifSpeed
- ifInOctets
- ifOutOctets
- ifInErrors
- ifOutErrors
- ifAdminStatus
- ifOperStatus

## 6. Flow traffic estimation

For each sampled packet:

```text
estimated_bytes =
  sampled_packet_length_bytes * sampling_rate
```

For an aggregation window:

```text
estimated_bps =
  sum(estimated_bytes) * 8 / window_seconds
```

This is a statistical estimate.

UI/API should expose it as sampled estimate, not exact throughput.

## 7. Counter rate calculation

```text
bps =
  (current_octets - previous_octets) * 8
  / elapsed_seconds
```

Handle:
- first sample: no rate yet
- counter reset
- interface restart
- impossible negative delta
- stale previous sample
- 32-bit wrap where relevant if field semantics require it

## 8. Sampling-rate changes

Sampling rate may vary by exporter/interface over time.

Never assume one global sampling rate.

Each normalized observation must carry the rate that applies to it.

## 9. Multiple observation points

The same original traffic may produce samples on:
- access switch
- distribution/core
- border

Do not naively add all of them into one "network total".

Use one of:
- explicitly configured observation scope
- boundary interface counters
- per-exporter/per-interface breakdown

The product should be able to say:
"observed on exporter X"
without claiming unique end-to-end traffic.

## 10. Internal/external classification

Use configured CIDRs.

Example:
- RFC1918
- local ULA
- public prefixes owned/used by the operator

Classification result:
- internal → internal
- internal → external
- external → internal
- external → external (possible depending on observation point)

## 11. Direction semantics

Do not assume "dst external = upload" in every topology.

Direction should be derived from:
- internal/external classification
- observation interface context where available
- configured boundary/interface role

For simple MVP:
- internal src + external dst = outbound
- external src + internal dst = inbound

But keep the model extensible.

## 12. Parser robustness

Requirements:
- bounds checking
- reject malformed lengths safely
- never panic on network input
- metrics for malformed/unsupported records
- rate-limited logs
- fuzz testing strongly encouraged for decoder

## 13. Metrics

Collector metrics should include:
- datagrams received
- datagrams dropped/invalid
- flow samples decoded
- counter samples decoded
- unsupported records
- parse errors
- processing lag
- aggregation queue depth

## 14. Implementation (Milestone C)

Packages: `internal/sflow` (XDR decode/encode), `internal/packet` (L2–L4 header parsing), `internal/collector` (UDP runtime, normalization, metrics, sinks), `cmd/collector`, `cmd/sflow-gen`.

### Supported structures (enterprise 0)

| Structure | Format | Use |
|---|---|---|
| flow_sample / expanded | 1 / 3 | sampling rate, input/output ifIndex, sequence, drops |
| counters_sample / expanded | 2 / 4 | interface counters |
| raw packet header | flow record 1 | Ethernet (incl. 802.1Q/QinQ), IPv4, IPv6 (+ extension headers), TCP/UDP ports |
| sampled_ipv4 / sampled_ipv6 | flow records 3 / 4 | fallback when no raw header is sent |
| extended_switch | flow record 1001 | VLAN when the header carries no tag |
| generic interface counters | counter record 1 | ifIndex, speed, status, in/out octets and errors |

Other formats are skipped via their length and counted (D-050).

### Normalized output

- `flow.Sample` (DATA_MODEL §3): `observed_at` (receive time, D-046), `exporter_id` = `agent/sub-agent` (D-049), input/output ifIndex (null when unknown, discarded or multiple), addresses, ports, protocol, VLAN, `sampled_packet_length` (`frame_length`, D-045), `sampling_rate`, `estimated_bytes`.
- `counters.Observation` (DATA_MODEL §4). `counters.Tracker` turns these into rates. sFlow octet counters are 64-bit, so a decrease is a reset.

### Running

```bash
pnpm dev:collector                                     # UDP :6343, metrics :9102
pnpm sflow-gen -target 127.0.0.1:6343 -scenario default   # demo exporter
curl -s localhost:9102/metrics
```

Example exporter configurations, to adapt to your devices:

```text
# host-sflow (hsflowd), /etc/hsflowd.conf
sflow {
  sampling = 400
  polling = 20
  collector { ip = <collector-ip> udpport = 6343 }
}

# Open vSwitch
ovs-vsctl -- --id=@s create sflow agent=<iface> target="\"<collector-ip>:6343\"" \
  header=128 sampling=512 polling=20 -- set bridge <bridge> sflow=@s
```

### Verification

- Hand-laid byte fixture from the specification, decoded field by field. It does not depend on the encoder.
- Encoder/decoder round trips: compact and expanded samples, IPv4 and IPv6 agents.
- Every prefix of a valid datagram decodes without panicking. Corrupted length fields map to typed errors.
- Go fuzzing of `sflow.Decode` and `packet.ParseEthernet` (`pnpm fuzz`).
- End to end: mock → `sflow-gen` → UDP → collector. Per-flow sample counts and estimated bytes match the mock exactly.
