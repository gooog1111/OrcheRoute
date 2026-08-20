#!/usr/bin/env sh
set -eu

pattern='^(go\.mod$|go\.sum$|cmd/|internal/|mobilecore/|webui/|desktop/|android/app/|android/build\.gradle$|android/settings\.gradle$|scripts/build-all\.(ps1|sh)$|\.github/workflows/cross-platform\.yml$)'

for path in \
  internal/mobile/parser/parser.go \
  mobilecore/mobilecore.go \
  android/app/src/main/AndroidManifest.xml \
  desktop/main.go \
  webui/app/page.tsx \
  go.mod
do
  printf '%s\n' "$path" | grep -Eq "$pattern" || {
    echo "expected build-sensitive path was not matched: $path" >&2
    exit 1
  }
done

for path in README.md ANDROID_ARCHITECTURE.md docs/note.txt
do
  if printf '%s\n' "$path" | grep -Eq "$pattern"; then
    echo "documentation-only path unexpectedly matched: $path" >&2
    exit 1
  fi
done
