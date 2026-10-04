# Network Traffic Visualizer — web UI
# Build context: repository root.
#   docker compose -f deploy/docker-compose.dev.yml up --build web

FROM node:22-alpine AS base
RUN corepack enable && corepack prepare pnpm@10.28.0 --activate
WORKDIR /repo

FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/
RUN pnpm install --frozen-lockfile

FROM deps AS build
COPY apps/web apps/web
# NEXT_PUBLIC_* values are inlined into the client bundle at build time,
# so changing them requires a rebuild.
ARG NEXT_PUBLIC_DATA_MODE=mock
ARG NEXT_PUBLIC_MOCK_SEED=42
ARG NEXT_PUBLIC_MOCK_SCENARIO=default
ARG NEXT_PUBLIC_MOCK_SPEED=1
ARG NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
ENV NEXT_PUBLIC_DATA_MODE=$NEXT_PUBLIC_DATA_MODE \
    NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL \
    NEXT_PUBLIC_MOCK_SEED=$NEXT_PUBLIC_MOCK_SEED \
    NEXT_PUBLIC_MOCK_SCENARIO=$NEXT_PUBLIC_MOCK_SCENARIO \
    NEXT_PUBLIC_MOCK_SPEED=$NEXT_PUBLIC_MOCK_SPEED \
    NEXT_OUTPUT_STANDALONE=1 \
    NEXT_TELEMETRY_DISABLED=1
RUN pnpm --filter web build

FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production \
    PORT=3000 \
    HOSTNAME=0.0.0.0 \
    NEXT_TELEMETRY_DISABLED=1
RUN addgroup -S app && adduser -S app -G app
COPY --from=build --chown=app:app /repo/apps/web/.next/standalone ./
COPY --from=build --chown=app:app /repo/apps/web/.next/static ./apps/web/.next/static
COPY --from=build --chown=app:app /repo/apps/web/public ./apps/web/public
USER app
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD wget -qO- http://127.0.0.1:3000/globe >/dev/null || exit 1
CMD ["node", "apps/web/server.js"]
