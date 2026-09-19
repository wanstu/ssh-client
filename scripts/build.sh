#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DESKTOP="$ROOT/cmd/ssh-client-desktop"
ICON_SOURCE="$DESKTOP/assets/appicon.png"
BUILD_DIR="$DESKTOP/build"
APP_ICON="$BUILD_DIR/appicon.png"

cd "$ROOT"

go run github.com/wanstu/wails-desktop-kit/cmd/desktopkit icon generate \
  --output "$ICON_SOURCE" \
  --symbol terminal \
  --background "#2463EB"

mkdir -p "$BUILD_DIR"
cp "$ICON_SOURCE" "$APP_ICON"

node --check cmd/ssh-client-desktop/frontend/app.js
go test ./...
go vet ./...

cd "$DESKTOP"
case "$(uname -s)" in
  Linux)
    wails build -clean -tags webkit2_41
    ;;
  Darwin)
    wails build -clean -platform darwin/universal
    ;;
  *)
    echo "Unsupported Unix platform: $(uname -s)" >&2
    exit 1
    ;;
esac
