#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
target="${1:-android}"
agenthub_transport_flags=""
if [[ "${AGENTHUB_ALLOW_LAN_HTTP:-false}" == true ]]; then
  agenthub_transport_flags='-X github.com/monkeyWie/reign/internal/device.AllowLANHTTP=true'
fi
if [[ "$target" == android ]]; then
  sdk="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-$HOME/Library/Android/sdk}}"
  ndk="${AGENTHUB_NDK:-$sdk/ndk/28.2.13676358}"
  case "$(uname -s)" in Darwin) host=darwin-x86_64;; Linux) host=linux-x86_64;; *) echo 'Run with an Android NDK toolchain on macOS/Linux'; exit 1;; esac
  for abi in arm64-v8a x86_64; do
    case "$abi" in arm64-v8a) arch=arm64; triplet=aarch64-linux-android;; x86_64) arch=amd64; triplet=x86_64-linux-android;; esac
    dest="clients/flutter/android/app/src/main/jniLibs/$abi"
    mkdir -p "$dest"
    CGO_ENABLED=1 GOOS=android GOARCH="$arch" CC="$ndk/toolchains/llvm/prebuilt/$host/bin/${triplet}24-clang" \
      go build -trimpath -ldflags="-checklinkname=0 $agenthub_transport_flags" -buildmode=c-shared -o "$dest/libagenthub.so" ./cmd/agenthub-bridge
    rm -f "$dest/libagenthub.h"
  done
elif [[ "$target" == ios ]]; then
  out="$PWD/build/ios-go"
  mkdir -p "$out/device" "$out/simulator"
  for kind in device simulator; do
    if [[ "$kind" == device ]]; then sdk=iphoneos; triple=arm64-apple-ios15.0; else sdk=iphonesimulator; triple=arm64-apple-ios15.0-simulator; fi
    sdkpath="$(xcrun --sdk "$sdk" --show-sdk-path)"
    compiler="$(xcrun --sdk "$sdk" --find clang)"
    CGO_ENABLED=1 GOOS=ios GOARCH=arm64 CC="$compiler" \
      CGO_CFLAGS="-target $triple -isysroot $sdkpath" CGO_LDFLAGS="-target $triple -isysroot $sdkpath" \
      go build -trimpath -ldflags="$agenthub_transport_flags" -buildmode=c-archive -o "$out/$kind/libAgentHubGo.a" ./cmd/agenthub-bridge
    mkdir -p "$out/$kind/headers"
    mv "$out/$kind/libAgentHubGo.h" "$out/$kind/headers/"
  done
  framework="$PWD/clients/flutter/ios/AgentHubGo/AgentHubGo.xcframework"
  if [[ -d "$framework" ]]; then mv "$framework" "$out/previous-$(date +%s).xcframework"; fi
  xcodebuild -create-xcframework \
    -library "$out/device/libAgentHubGo.a" -headers "$out/device/headers" \
    -library "$out/simulator/libAgentHubGo.a" -headers "$out/simulator/headers" \
    -output "$framework"
else echo 'Usage: scripts/build-go-mobile.sh android|ios'; exit 1; fi
