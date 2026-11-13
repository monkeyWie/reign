#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ $# -eq 0 ]]; then set -- macos; fi
exec ./scripts/build-mvp.sh "$@"
