#!/bin/bash
set -e

cd "$(dirname "$0")"

APP_NAME="Hibi"
BUNDLE="${APP_NAME}.app"

echo "==> Compiling ${APP_NAME}.swift"
swiftc -parse-as-library -O "${APP_NAME}.swift" -o "${APP_NAME}"

echo "==> Assembling ${BUNDLE}"
rm -rf "${BUNDLE}"
mkdir -p "${BUNDLE}/Contents/MacOS"
cp "${APP_NAME}" "${BUNDLE}/Contents/MacOS/${APP_NAME}"

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

echo "==> Done. Built ${BUNDLE} and standalone binary ./${APP_NAME}"
