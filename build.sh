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
  echo "==> Updating ${APP_NAME}.app"
  killall "${APP_NAME}" 2>/dev/null || true
  rm -rf "${APP_NAME}.app"
  ditto "${BUILT}" "${APP_NAME}.app"
  open "${APP_NAME}.app"
  echo "==> Done: ./${APP_NAME}, ./${APP_NAME}.app, and ${BUILT}"
else
  echo "==> Done. MyGo artifacts are in build/"
fi
