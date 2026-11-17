package main

import _ "embed"

// trayIconPNG is the menu bar icon: a cat's head, drawn to 32 pixels (16
// points at 2x) of black on transparent by scripts/gen-tray-icon.swift. IconIsTemplate has macOS tint it for the menu
// bar, so the one file fits a light and a dark menu bar alike.
//
//go:embed icon/tray.png
var trayIconPNG []byte
