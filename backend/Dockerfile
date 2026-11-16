FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/rentflow ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/rentflow /app/rentflow
COPY migrations /app/migrations
# Fail fast with a readable message instead of a stack trace when the runtime
# environment is incomplete (JWT_SECRET, DATABASE_URL, TLS settings).
ENV APP_ENV=production \
    PORT=3000 \
    COOKIE_SECURE=true \
    SMS_PROVIDER=mock
EXPOSE 3000
USER 10001:10001
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:3000/healthz || exit 1
CMD ["/app/rentflow"]
