#!/bin/bash
set -e

cd "$(dirname "$0")"

APP_NAME="Hibi"
BUNDLE="${APP_NAME}.app"

# Quit any running instance first: rebuilding deletes and recreates the bundle,
# and a stale process keeps posting notifications whose icon then resolves to the
# macOS placeholder because LaunchServices still points at the deleted bundle.
echo "==> Quitting any running ${APP_NAME} instance"
killall "${APP_NAME}" 2>/dev/null || true

echo "==> Compiling ${APP_NAME}.swift"
swiftc -parse-as-library -O "${APP_NAME}.swift" -o "${APP_NAME}"

echo "==> Generating app icon"
iconutil -c icns icon/AppIcon.iconset -o icon/AppIcon.icns

echo "==> Assembling ${BUNDLE}"
rm -rf "${BUNDLE}"
mkdir -p "${BUNDLE}/Contents/MacOS"
mkdir -p "${BUNDLE}/Contents/Resources"
cp "${APP_NAME}" "${BUNDLE}/Contents/MacOS/${APP_NAME}"
cp icon/AppIcon.icns "${BUNDLE}/Contents/Resources/AppIcon.icns"

cat > "${BUNDLE}/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleDisplayName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleIdentifier</key>
    <string>io.github.uncertaintydeterminesyou4ndme.hibi</string>
    <key>CFBundleVersion</key>
    <string>1.0</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleExecutable</key>
    <string>${APP_NAME}</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>LSUIElement</key>
    <true/>
    <key>LSMinimumSystemVersion</key>
    <string>13.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
EOF

echo "==> Code signing (ad-hoc)"
codesign --force --sign - "${BUNDLE}"

echo "==> Relaunching ${BUNDLE} (re-registers the bundle so the icon is picked up)"
open "${BUNDLE}"

echo "==> Done. Built ${BUNDLE} and standalone binary ./${APP_NAME}"
