#!/usr/bin/env sh
set -eu

target="${1:-android}"
root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
mkdir -p "$root/dist"
cd "$root"

case "$target" in
  android)
    gomobile bind -target=android -androidapi=21 -o dist/OrcheRouteCore.aar ./mobilecore
    ;;
  ios)
    if [ "$(uname -s)" != "Darwin" ]; then
      echo "iOS XCFramework requires macOS and Xcode" >&2
      exit 2
    fi
    gomobile bind -target=ios -o dist/OrcheRouteCore.xcframework ./mobilecore
    ;;
  *)
    echo "usage: $0 android|ios" >&2
    exit 2
    ;;
esac
