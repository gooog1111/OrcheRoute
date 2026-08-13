#!/usr/bin/env bash
set -euo pipefail

target="${1:-all}"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
exec go run ./cmd/orcheroute-build -target "$target"
