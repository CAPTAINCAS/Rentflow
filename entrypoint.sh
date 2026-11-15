#!/usr/bin/env bash
set -uo pipefail

# Single-container gateway defaults. The API process is the public entry point
# and reverse-proxies page requests to the Next.js process on port 3001.
export COOKIE_SECURE="${COOKIE_SECURE:-true}"
export WEB_ORIGIN="${WEB_ORIGIN:-http://127.0.0.1:3001}"
export MIGRATIONS_DIR="${MIGRATIONS_DIR:-/app/migrations}"

(
  cd /app/frontend
  HOSTNAME=127.0.0.1 PORT=3001 node server.js
) &
web_pid=$!

(
  cd /app
  PORT=3000 /app/rentflow-api
) &
api_pid=$!

terminate() {
  kill -TERM "$api_pid" "$web_pid" 2>/dev/null || true
  wait "$api_pid" 2>/dev/null || true
  wait "$web_pid" 2>/dev/null || true
}
trap terminate TERM INT

set +e
wait -n "$api_pid" "$web_pid"
status=$?
terminate
exit "$status"
