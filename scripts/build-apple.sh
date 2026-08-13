#!/usr/bin/env bash
set -euo pipefail

target="${1:-all}"
root="$(cd "$(dirname "$0")/.." && pwd)"
apple="$root/apple"
dist="$root/dist/apple"
frameworks="$apple/Frameworks"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "OrcheRoute Apple requires macOS with Xcode" >&2
  exit 2
fi
command -v xcodebuild >/dev/null || { echo "xcodebuild not found" >&2; exit 2; }
command -v xcodegen >/dev/null || { echo "xcodegen not found (brew install xcodegen)" >&2; exit 2; }
command -v gomobile >/dev/null || { echo "gomobile not found" >&2; exit 2; }

mkdir -p "$dist" "$frameworks"
cd "$root"
gomobile bind -tags="with_gvisor,cmfa" -target=ios -o "$frameworks/OrcheRouteCore.xcframework" ./mobilecore

cd "$apple"
xcodegen generate

team="${ORCHEROUTE_DEVELOPMENT_TEAM:-}"
if [[ -z "$team" && -f Config/Signing.xcconfig ]]; then
  team="$(sed -n 's/^[[:space:]]*ORCHEROUTE_DEVELOPMENT_TEAM[[:space:]]*=[[:space:]]*//p' Config/Signing.xcconfig | tail -1 | tr -d '[:space:]')"
fi

build_ios() {
  if [[ -n "$team" ]]; then
    xcodebuild -project OrcheRouteApple.xcodeproj -scheme OrcheRoute-iOS -configuration Release \
      -destination 'generic/platform=iOS' -archivePath "$dist/OrcheRoute-iOS.xcarchive" \
      DEVELOPMENT_TEAM="$team" archive
  else
    xcodebuild -project OrcheRouteApple.xcodeproj -scheme OrcheRoute-iOS -configuration Debug \
      -sdk iphonesimulator -destination 'generic/platform=iOS Simulator' \
      -derivedDataPath "$dist/DerivedData-iOS" CODE_SIGNING_ALLOWED=NO build
  fi
}

build_macos() {
  if [[ -n "$team" ]]; then
    xcodebuild -project OrcheRouteApple.xcodeproj -scheme OrcheRoute-macOS -configuration Release \
      -destination 'generic/platform=macOS' -archivePath "$dist/OrcheRoute-macOS.xcarchive" \
      DEVELOPMENT_TEAM="$team" archive
  else
    xcodebuild -project OrcheRouteApple.xcodeproj -scheme OrcheRoute-macOS -configuration Debug \
      -destination 'generic/platform=macOS' -derivedDataPath "$dist/DerivedData-macOS" \
      CODE_SIGNING_ALLOWED=NO build
  fi
}

case "$target" in
  ios) build_ios ;;
  macos) build_macos ;;
  all) build_ios; build_macos ;;
  *) echo "usage: $0 ios|macos|all" >&2; exit 2 ;;
esac
