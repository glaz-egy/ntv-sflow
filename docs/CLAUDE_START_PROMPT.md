# Claude Code Start Prompt

Use this prompt after placing this package in the repository.

---

Read `CLAUDE.md` and all documents under `docs/` before changing code.

The repository describes a visualization-first network traffic application with two connected primary views:

1. Globe View — external destinations on a 3D globe.
2. Home Network View — internal device communications on an interactive graph.

Do not start by implementing the collector.

First:

1. Review all design documents and list any contradictions or missing assumptions.
2. Do not change accepted decisions in `docs/DECISIONS.md` without explaining why.
3. Propose the concrete repository structure based on the documented target.
4. Define the shared frontend/API domain contracts.
5. Implement deterministic Mock Mode that drives both Globe and Home from the same underlying traffic model.
6. Implement the application shell and Globe View.
7. Implement Home Network View.
8. Implement cross-view navigation preserving filters and selection context.
9. Add tests for state transitions and mock consistency.
10. Only then start the live backend and sFlow collector.

For UI:
- keep the visualization area dominant
- use dark theme
- avoid a Grafana-style dashboard
- do not replace the globe with a 2D map
- do not replace the home graph with tables
- do not imply that GeoIP arcs are physical packet routes
- do not imply exact throughput for sampled flow estimates

For architecture:
- Go backend/collector
- Next.js + TypeScript frontend
- globe.gl preferred for Globe
- React Flow preferred for Home
- PostgreSQL metadata
- ClickHouse history
- Redis optional

When you need to make a non-trivial assumption, record it in `docs/DECISIONS.md`.

Begin by producing:
- an implementation plan,
- the final repository tree,
- the first milestone task list,
then scaffold the repository and begin Mock Mode.
