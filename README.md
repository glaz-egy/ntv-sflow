# Network Traffic Visualizer

A visualization-first network traffic explorer built around two connected views:

- **Globe View**: see where external traffic goes on an interactive 3D globe.
- **Home Network View**: see which internal devices communicate with whom.

This is a working project title. The final product name can be changed later.

## Why this exists

Traditional monitoring tools are excellent at charts and counters, but they are not optimized for visually answering:

- Where in the world is my network talking to?
- Which local device is responsible?
- Which devices are communicating internally?
- What is the relative traffic magnitude right now?
- Can I jump from an external destination back to the responsible internal device?

This project makes those questions the center of the UI.

## MVP architecture

```text
Network devices
   │
   │ sFlow v5
   ▼
Collector (Go)
   │
   ├── flow samples
   ├── counter samples
   └── normalized observations
   ▼
Aggregation / Enrichment
   ├── internal/external classification
   ├── GeoIP / ASN
   ├── device metadata
   └── live windows
   │
   ├──────────────► REST / WebSocket API
   │                         │
   │                         ▼
   │                  Next.js frontend
   │                   ├── Globe View
   │                   └── Home View
   │
   ├── PostgreSQL
   └── ClickHouse
```

## Repository layout

```text
.
├── CLAUDE.md
├── README.md
├── .env.example
├── configs/
│   └── config.example.yaml
├── deploy/
│   ├── docker-compose.dev.yml
│   └── k8s/            # Kustomize manifests (base / overlays/live / overlays/demo)
├── docs/
│   ├── PRODUCT_SPEC.md
│   ├── ARCHITECTURE.md
│   ├── DATA_MODEL.md
│   ├── SFLOW.md
│   ├── API.md
│   ├── GLOBE_VIEW.md
│   ├── HOME_NETWORK_VIEW.md
│   ├── DEVELOPMENT.md
│   ├── DECISIONS.md
│   ├── MOCK_DATA.md
│   ├── SECURITY.md
│   ├── OBSERVABILITY.md
│   └── TASKS.md
├── apps/
│   └── web/            # Next.js app (Globe/Home views, contracts, in-browser mock backend)
├── api/openapi.yaml    # API contract (source of truth)
├── cmd/
│   ├── collector/
│   └── api/
└── internal/
```

## Quick start (mock mode)

日本語のセットアップ手順（Docker あり／なし）: `docs/SETUP.ja.md`
Kubernetes へのデプロイ: `docs/KUBERNETES.ja.md`（マニフェストは `deploy/k8s/`）

Requirements: Node.js 20+ and pnpm 10 (Go 1.27+ for the API and `pnpm test`).

```bash
pnpm install
pnpm dev          # http://localhost:3000 → /globe (mock data generated in the browser)
pnpm dev:api      # Go API on :8080 (REST + WebSocket, same mock data)
pnpm dev:collector                        # sFlow v5 collector on UDP :6343, metrics on :9102
pnpm sflow-gen -target 127.0.0.1:6343     # demo exporter: sFlow generated from the mock
pnpm dev:live-demo                        # LIVE mode: API + collector, demo inventory (pair with sflow-gen)
pnpm test         # vitest + go test
pnpm lint && pnpm typecheck
```

To make the UI use the Go API, set `NEXT_PUBLIC_DATA_MODE=api` in `apps/web/.env.local`.
The API contract is `api/openapi.yaml` (TS types: `pnpm gen:api`).

Mock mode needs no collector, database or GeoIP file. Pick a scenario with
`NEXT_PUBLIC_MOCK_SCENARIO` (`default`, `heavy-download`, `internal-backup`,
`many-destinations`, `unknown-metadata`, `stale-collector`), or switch it in the
Settings menu. `NEXT_PUBLIC_MOCK_SEED` and `NEXT_PUBLIC_MOCK_SPEED` are also read.

Views share their investigation context through the URL, e.g.
`/home?dst=asn:13335` (sources of Cloudflare traffic) or
`/globe?src=dev_pc01&proto=tcp` (pc-01's external TCP destinations).

With Docker (no Node.js/Go needed on the host):

```bash
docker compose -f deploy/docker-compose.dev.yml up --build -d web
WEB_DATA_MODE=api docker compose -f deploy/docker-compose.dev.yml up --build -d web api
docker compose -f deploy/docker-compose.dev.yml --profile demo up --build -d collector sflow-gen
```

Implementation status and plan: `docs/IMPLEMENTATION_PLAN.md`.

## Start here

Claude Code should read:

1. `CLAUDE.md`
2. `docs/PRODUCT_SPEC.md`
3. `docs/ARCHITECTURE.md`
4. `docs/DATA_MODEL.md`
5. `docs/API.md`
6. Both view specifications
7. `docs/TASKS.md`

## MVP priorities

1. Mock-mode Globe View.
2. Mock-mode Home Network View.
3. Cross-navigation between both views.
4. Shared API contracts.
5. Live WebSocket aggregate transport.
6. sFlow v5 ingestion.
7. GeoIP/ASN enrichment.
8. Persistent history.

## Not MVP-blocking

- Full SNMP discovery
- LLDP/CDP automatic topology
- Cisco WLC integration
- Kubernetes Pod-level topology
- vSphere integration
- Advanced alerting
- Multi-tenancy
- HA collectors
- Full historical replay

## Development configuration

Copy:

```bash
cp .env.example .env
```

Use `configs/config.example.yaml` as the initial runtime configuration reference.

The provided compose file is intentionally a development scaffold. Claude Code should refine service names, health checks, volumes and schemas as implementation progresses.
