FROM golang:1.22-bookworm AS api-build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/rentflow-api ./cmd/api

FROM node:22-bookworm-slim AS web-deps
RUN corepack enable && corepack prepare pnpm@10.18.0 --activate
WORKDIR /app/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

FROM web-deps AS web-build
WORKDIR /app/frontend
COPY frontend/ ./
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm build

FROM node:22-bookworm-slim AS runtime
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates bash \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
ENV NODE_ENV=production \
    APP_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    COOKIE_SECURE=true
COPY --from=api-build --chown=node:node /out/rentflow-api /app/rentflow-api
COPY --from=api-build --chown=node:node /src/backend/migrations /app/backend/migrations
COPY --from=web-build --chown=node:node /app/frontend/.next/standalone/ /app/frontend/
COPY --from=web-build --chown=node:node /app/frontend/.next/static/ /app/frontend/.next/static/
COPY --from=web-build --chown=node:node /app/frontend/public/ /app/frontend/public/
COPY --chown=node:node entrypoint.sh /app/entrypoint.sh
RUN chmod 755 /app/entrypoint.sh
USER node
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s --start-period=45s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:3000/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
ENTRYPOINT ["/app/entrypoint.sh"]
