#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ ! -x build/agenthub-service || ! -d build/AgentHub.app ]]; then
  scripts/build-mvp.sh macos
fi
if command -v curl >/dev/null && [[ "$(curl -fsS --max-time 1 http://127.0.0.1:26331/healthz 2>/dev/null || true)" == ok ]]; then
  open "$PWD/build/AgentHub.app"
  echo 'Using the running device service at http://127.0.0.1:26331'
  exit 0
fi
mkdir -p build/local-service
build/agenthub-service --state build/local-service/devices.json --web clients/flutter/build/web &
service_pid=$!
trap 'kill "$service_pid" 2>/dev/null || true' EXIT INT TERM
open "$PWD/build/AgentHub.app"
printf 'Device service: http://127.0.0.1:26331\nWeb: build with scripts/build-mvp.sh web, then visit the same address.\nSet this URL in the desktop server settings. Ctrl+C stops the service.\n'
wait "$service_pid"
