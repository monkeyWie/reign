#!/bin/sh
# NSGo owns its linker arguments. Add this client's compatibility / default
# configuration flags without changing the framework, Go cache or backend.
nsgo_tool=$1
shift
case "$nsgo_tool" in
  */link)
    if [ "${GOOS:-}" = android ]; then
      set -- -checklinkname=0 "$@"
    fi
    if [ -n "${AGENTHUB_SERVER_URL:-}" ]; then
      set -- "-X=github.com/monkeyWie/reignhub/clients/nsgo/hubui.DefaultService=$AGENTHUB_SERVER_URL" "$@"
    fi
    if [ "${AGENTHUB_ALLOW_LAN_HTTP:-false}" = true ]; then
      set -- "-X=github.com/monkeyWie/reignhub/internal/device.AllowLANHTTP=true" "$@"
      set -- "-X=github.com/monkeyWie/reignhub/clients/nsgo/browserclient.AllowLANHTTP=true" "$@"
    fi
    exec "$nsgo_tool" "$@"
    ;;
  *) exec "$nsgo_tool" "$@" ;;
esac
