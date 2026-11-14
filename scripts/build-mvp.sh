#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
flutter_bin="${FLUTTER_BIN:-flutter}"
agenthub_web_output="${AGENTHUB_WEB_OUTPUT:-build/web}"
platform="${1:-macos}"
agenthub_flutter_args=("${@:2}")
agenthub_go_ldflags=""
if [[ "$platform" != web && -n "${AGENTHUB_SERVICE:-}" ]]; then
  agenthub_flutter_args+=("--dart-define=AGENTHUB_SERVICE=$AGENTHUB_SERVICE")
fi
if [[ "${AGENTHUB_ALLOW_LAN_HTTP:-false}" == true ]]; then
  agenthub_flutter_args+=("--dart-define=AGENTHUB_ALLOW_LAN_HTTP=true")
  agenthub_go_ldflags='-X github.com/monkeyWie/reignhub/internal/device.AllowLANHTTP=true'
fi
mkdir -p build
if [[ "$platform" == macos ]]; then
  python3 scripts/package-macos.py prepare
fi
CGO_ENABLED=0 go build -trimpath -o build/agenthub-service ./cmd/agenthub-service
case "$platform" in
  macos)
    CGO_ENABLED=1 go build -trimpath -ldflags="$agenthub_go_ldflags" -buildmode=c-shared -o build/libagenthub.dylib ./cmd/agenthub-bridge
    (cd clients/flutter && "$flutter_bin" build macos --release ${agenthub_flutter_args[@]+"${agenthub_flutter_args[@]}"})
    python3 scripts/package-macos.py package;;
  web)
    # Flutter 3.41's cached web entrypoint does not track plugin dependencies.
    # Regenerate it so newly added file-picker/drop plugins are registered.
    python3 - <<'PYWEB'
from pathlib import Path
for stamp in Path('clients/flutter/.dart_tool/flutter_build').glob('*/web_entrypoint.stamp'):
    stamp.unlink()
PYWEB
    (cd clients/flutter && "$flutter_bin" build web --release --no-web-resources-cdn --output "$agenthub_web_output" ${agenthub_flutter_args[@]+"${agenthub_flutter_args[@]}"})
    AGENTHUB_WEB_OUTPUT="$agenthub_web_output" python3 - <<'PYWEB'
import os
from pathlib import Path
output = Path(os.environ['AGENTHUB_WEB_OUTPUT'])
if not output.is_absolute():
    output = Path('clients/flutter') / output
bundle = (output / 'main.dart.js').read_text()
if '__file_picker_web-file-input' not in bundle or 'desktop_drop' not in bundle:
    raise SystemExit('Web upload plugin registration is missing from the release build')
PYWEB
    ;;
  android) scripts/build-go-mobile.sh android; (cd clients/flutter && "$flutter_bin" build apk --release ${agenthub_flutter_args[@]+"${agenthub_flutter_args[@]}"});;
  ios) scripts/build-go-mobile.sh ios; (cd clients/flutter && "$flutter_bin" build ios --release --no-codesign ${agenthub_flutter_args[@]+"${agenthub_flutter_args[@]}"});;
  *) echo 'Usage: scripts/build-mvp.sh macos|web|android|ios'; exit 1;;
esac
