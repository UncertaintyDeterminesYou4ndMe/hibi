#!/bin/bash
# 把 hibi 装进「登录即启动」：写 LaunchAgent 并立即加载。
set -euo pipefail

APP="/Applications/Hibi.app/Contents/MacOS/Hibi" # build.sh installs it there
PLIST="$HOME/Library/LaunchAgents/io.github.hibi.startatlogin.plist"
LABEL="io.github.hibi.startatlogin"

[ -x "$APP" ] || { echo "找不到 $APP，先 bash build.sh"; exit 1; }

cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$APP</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
</dict>
</plist>
EOF

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
echo "hibi 已设为登录启动"
