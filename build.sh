#!/bin/sh
set -eu

cd "$(dirname "$0")"
APP_NAME="Hibi"

echo "==> Running Go tests"
go test ./...

echo "==> Building standalone ${APP_NAME}"
go build -o "${APP_NAME}" .
"./${APP_NAME}" --selftest

echo "==> Building and signing with MyGo"
go tool mygo build -skip-dmg

if [ "$(go env GOOS)" = "darwin" ]; then
  BUILT="build/darwin-$(go env GOARCH)/${APP_NAME}.app"
  # Installed in /Applications, where Spotlight and Launchpad find it, and
  # where the login item (scripts/install-launchagent.sh) opens it.
  INSTALLED="/Applications/${APP_NAME}.app"
  echo "==> Installing ${INSTALLED}"
  killall "${APP_NAME}" 2>/dev/null || true
  rm -rf "${INSTALLED}"
  ditto "${BUILT}" "${INSTALLED}"
  open "${INSTALLED}"
  echo "==> Done: ./${APP_NAME}, ${BUILT} and ${INSTALLED}"
else
  echo "==> Done. MyGo artifacts are in build/"
fi
