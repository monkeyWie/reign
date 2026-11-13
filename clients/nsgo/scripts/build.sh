#!/bin/sh
set -eu
nsgo_client_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$nsgo_client_dir"
# A temporary path avoids GOFLAGS quoting problems if the checkout has spaces.
nsgo_exec_wrapper=$(mktemp /tmp/agenthub-nsgo-toolexec.XXXXXX)
trap 'rm -f "$nsgo_exec_wrapper"' EXIT HUP INT TERM
cp scripts/go-toolexec.sh "$nsgo_exec_wrapper"
chmod +x "$nsgo_exec_wrapper"
export GOWORK=off
export GOFLAGS="${GOFLAGS:-} -toolexec=$nsgo_exec_wrapper"
if [ "$#" -gt 0 ]; then
  exec_target=$1
  shift
  # Preserve the regular NSGo release layout; Android then adds the host using
  # the framework's public embedding APIs and re-signs with the same key.
  nsgo_output=dist
  nsgo_prev=
  for nsgo_arg in "$@"; do
    if [ "$nsgo_prev" = -output ]; then nsgo_output=$nsgo_arg; fi
    case "$nsgo_arg" in -output=*) nsgo_output=${nsgo_arg#-output=};; esac
    nsgo_prev=$nsgo_arg
  done
  go tool nsgo build -target "$exec_target" "$@"
  case "$exec_target" in *android*) python3 scripts/build-android-host.py --targets "$exec_target" --output "$nsgo_output";; esac
else
  go tool nsgo build
  python3 scripts/build-android-host.py --targets android_arm64 --output dist
fi
