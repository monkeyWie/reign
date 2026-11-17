#!/bin/sh
set -eu
reignhub_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
nsgo_client_dir="$reignhub_root/clients/nsgo"
nsgo_invocation_dir=$PWD
cd "$nsgo_client_dir"
# A temporary path avoids GOFLAGS quoting problems if the checkout has spaces.
nsgo_exec_wrapper=$(mktemp /tmp/agenthub-nsgo-toolexec.XXXXXX)
trap 'rm -f "$nsgo_exec_wrapper"' EXIT HUP INT TERM
cp "$reignhub_root/scripts/go-toolexec.sh" "$nsgo_exec_wrapper"
chmod +x "$nsgo_exec_wrapper"
export GOWORK=off
export GOFLAGS="${GOFLAGS:-} -toolexec=$nsgo_exec_wrapper"
if [ "$#" -gt 0 ]; then
  exec_target=$1
  shift
  # Preserve the regular NSGo release layout; Android then adds the host using
  # the framework's public embedding APIs and re-signs with the same key.
  nsgo_output="$reignhub_root/dist"
  nsgo_version=
  nsgo_build_number=
  nsgo_prev=
  for nsgo_arg in "$@"; do
    case "$nsgo_prev" in
      -output) nsgo_output=$nsgo_arg ;;
      -version) nsgo_version=$nsgo_arg ;;
      -build-number) nsgo_build_number=$nsgo_arg ;;
    esac
    case "$nsgo_arg" in
      -output=*) nsgo_output=${nsgo_arg#-output=} ;;
      -version=*) nsgo_version=${nsgo_arg#-version=} ;;
      -build-number=*) nsgo_build_number=${nsgo_arg#-build-number=} ;;
    esac
    nsgo_prev=$nsgo_arg
  done
  # Interpret an explicit relative output from the caller's directory, before
  # entering the separate NSGo module. The default always belongs to the repo.
  case "$nsgo_output" in /*) ;; *) nsgo_output="$nsgo_invocation_dir/$nsgo_output" ;; esac
  export REIGNHUB_BUILD_VERSION="$nsgo_version" REIGNHUB_BUILD_NUMBER="$nsgo_build_number"
  go tool nsgo build -target "$exec_target" "$@" -output "$nsgo_output"
  case "$exec_target" in *android*) python3 "$reignhub_root/scripts/build-android-host.py" --targets "$exec_target" --output "$nsgo_output";; esac
else
  go tool nsgo build -output "$reignhub_root/dist"
  python3 "$reignhub_root/scripts/build-android-host.py" --targets android_arm64 --output "$reignhub_root/dist"
fi
