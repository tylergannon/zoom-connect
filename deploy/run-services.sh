#!/bin/bash
set -euo pipefail
children=()
cleanup() {
  trap - EXIT TERM INT
  if ((${#children[@]})); then kill "${children[@]}" 2>/dev/null || true; fi
  wait || true
}
trap cleanup EXIT TERM INT
LISTEN_ADDR=127.0.0.1:8081 /app/rtmsrec &
children+=("$!")
/app/zoom-connect -listen 0.0.0.0:8080 -origin "${PUBLIC_URL:?PUBLIC_URL is required}" &
children+=("$!")
# Either service exiting requires a fresh pair, including recorder sessions.
wait -n "${children[@]}"
exit 1
