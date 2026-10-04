# Development Guide

## 1. Strategy

Develop visualization-first using deterministic mock data, then connect live ingestion.

Recommended phases:

### Phase 0 — Scaffold
- initialize Go module/workspace
- initialize Next.js app
- configure lint/format/test
- add shared API schema strategy
- wire mock/live provider abstraction

### Phase 1 — Mock Globe
- render 3D globe
- mock destinations
- arcs
- marker selection
- inspector
- filters
- live mock updates

### Phase 2 — Mock Home
- render graph
- mock devices
- traffic/topology/hybrid
- node/edge inspector
- focus
- cross-view state

### Phase 3 — Backend contracts
- REST snapshots
- WebSocket envelopes
- mock backend adapter
- frontend hooks

### Phase 4 — sFlow collector
- UDP receive
- v5 decode
- flow sample normalization
- counter sample normalization
- tests/fuzz

### Phase 5 — Aggregation/enrichment
- windows
- CIDR classification
- GeoIP/ASN
- device registry
- counter rates

### Phase 6 — Persistence
- PostgreSQL
- ClickHouse
- migrations/schema
- retention

## 2. Local development modes

### Mock mode
No collector required.

Must support:
- Globe animation
- Home graph
- navigation
- filters
- inspector
- status "MOCK"

### Live mode
Requires backend/collector.

UI must expose:
- collector state
- last received timestamp
- stale state

## 3. Frontend structure suggestion

```text
apps/web/
  app/
    globe/
    home/
    settings/
  components/
    shell/
    filters/
    inspector/
    globe/
    home/
  lib/
    api/
    realtime/
    state/
    formatting/
    mock/
  types/
```

Avoid putting entire views in one component.

## 4. Backend structure suggestion

```text
cmd/
  collector/
  api/

internal/
  sflow/
  flow/
  counters/
  aggregation/
  enrichment/
  devices/
  topology/
  realtime/
  storage/
  httpapi/
```

## 5. Shared contracts

Choose one:
- OpenAPI source of truth + generated clients/types
- JSON Schema source of truth
- carefully maintained matching Go/TS DTOs

Preferred:
- OpenAPI for REST
- documented WebSocket message schemas

Avoid ad hoc frontend assumptions.

## 6. Configuration

Config hierarchy:
1. defaults
2. YAML
3. environment overrides

Secrets:
- environment or secret store
- never commit

## 7. Formatting and linting

Frontend:
- ESLint
- Prettier

Go:
- gofmt
- go vet
- staticcheck if practical

## 8. Testing

### Go unit
- parser
- counter rate
- classifier
- aggregate math
- identity resolution

### Go fuzz
- sFlow decoder boundaries

### Frontend unit
- state
- filter logic
- cross-navigation state
- data transforms

### Frontend component/e2e
- Globe destination selection
- Home device selection
- Globe→Home
- Home→Globe

## 9. Performance development rule

Use representative synthetic load early.

Do not wait until final integration to test:
- 100 arcs
- 300 nodes
- several hundred candidate edges before filtering
- rapid WebSocket snapshots

## 10. Git workflow

Keep commits focused:
- scaffold
- contracts
- globe
- home
- collector
- aggregation
etc.

Avoid giant "implement everything" commits.

## 11. Documentation rule

When changing a core behavior:
- update relevant `docs/*.md`
- record architectural decisions in `DECISIONS.md`

## 12. Initial Claude Code instruction

Claude Code should first:
1. inspect all docs
2. identify contradictions
3. propose minimal adjustments
4. scaffold repository
5. implement mock provider
6. implement Globe UI
7. implement Home UI
before building deep ingestion infrastructure
