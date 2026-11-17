#!/bin/sh
# Install a published, checksum-verified server binary without root privileges.
set -eu
version=""
prefix="${REIGNHUB_INSTALL_PREFIX:-$HOME/.local/bin}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) version="$2"; shift 2 ;;
    --prefix) prefix="$2"; shift 2 ;;
    *) echo 'Usage: install-server.sh [--version X.Y.Z] [--prefix BIN_DIR]' >&2; exit 2 ;;
  esac
done
case "$(uname -s)" in Linux) platform=linux ;; Darwin) platform=darwin ;; *) echo 'Use the Windows ZIP or Scoop manifest on Windows.' >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo 'Unsupported CPU architecture.' >&2; exit 1 ;; esac
if [ -z "$version" ]; then
  version=$(curl -fsSL https://api.github.com/repos/monkeyWie/reignhub/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
fi
case "$version" in ''|*[!0-9A-Za-z.-]*) echo 'Invalid release version; specify --version X.Y.Z (including prereleases).' >&2; exit 1 ;; esac
name="reignhub-server-$version-$platform-$arch"
url="https://github.com/monkeyWie/reignhub/releases/download/v$version/$name.tar.gz"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
curl -fL "$url" -o "$work/$name.tar.gz"
curl -fsSL "$url.sha256" -o "$work/checksum"
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$work" && sha256sum -c checksum)
else
  (cd "$work" && shasum -a 256 -c checksum)
fi
tar -xzf "$work/$name.tar.gz" -C "$work"
mkdir -p "$prefix"
install -m 755 "$work/$name/reignhub-server" "$prefix/reignhub-server"
"$prefix/reignhub-server" --version
printf '\nInstalled to %s. Start with: %s/reignhub-server\n' "$prefix" "$prefix"
printf 'HTTP :26330; discovery TCP/UDP :26331. Run --help for configuration.\n'
