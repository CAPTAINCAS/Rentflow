#!/usr/bin/env bash
set -uo pipefail

export COOKIE_SECURE="${COOKIE_SECURE:-true}"
export WEB_ORIGIN="${WEB_ORIGIN:-http://127.0.0.1:3001}"

(
  cd /app/frontend
  HOSTNAME=127.0.0.1 PORT=3001 node server.js
) &
web_pid=$!

(
  cd /app/backend
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
