#!/bin/sh
set -eu

REPO="fadedreams/helm-blame"
DIR="${HELM_PLUGIN_DIR:-$(cd "$(dirname "$0")/.." && pwd)}"

# Developers: build bin/helm-blame yourself, then set this to skip the download.
if [ "${HELM_BLAME_SKIP_DOWNLOAD:-}" = "1" ]; then
  exit 0
fi

version=$(grep '^version:' "$DIR/plugin.yaml" | sed 's/version: *//; s/"//g')

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) echo "helm-blame: unsupported OS '$os' (on Windows, use WSL)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "helm-blame: unsupported architecture '$arch'" >&2; exit 1 ;;
esac

url="https://github.com/$REPO/releases/download/v${version}/helm-blame_${version}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "helm-blame: downloading $url"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$url" -o "$tmp/pkg.tar.gz"
elif command -v wget >/dev/null 2>&1; then
  wget -q "$url" -O "$tmp/pkg.tar.gz"
else
  echo "helm-blame: need curl or wget" >&2; exit 1
fi

tar -xzf "$tmp/pkg.tar.gz" -C "$tmp"
mkdir -p "$DIR/bin"
install -m 0755 "$tmp/helm-blame" "$DIR/bin/helm-blame"
echo "helm-blame: installed $DIR/bin/helm-blame"
