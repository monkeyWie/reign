#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p build
export CGO_ENABLED=0
go build -trimpath -o build/agenthub ./cmd/agenthub
go build -trimpath -o build/agenthub-relay ./cmd/agenthub-relay
if [[ "$(go env GOOS)" == darwin ]]; then
  bundle=build/AgentHub.app
  mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources"
  rm -f "$bundle/Contents/MacOS/agenthubd"
  cp build/agenthub "$bundle/Contents/MacOS/"
  cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>dev.agenthub.desktop</string>
<key>CFBundleName</key><string>Agent Hub</string>
<key>CFBundleDisplayName</key><string>Agent Hub</string>
<key>CFBundleExecutable</key><string>agenthub</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>0.1.0</string>
<key>CFBundleVersion</key><string>1</string>
<key>NSHighResolutionCapable</key><true/>
<key>LSMinimumSystemVersion</key><string>13.0</string>
</dict></plist>
PLIST
  codesign --force --deep --sign - "$bundle"
fi
