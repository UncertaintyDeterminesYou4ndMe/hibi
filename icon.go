package main

import _ "embed"

// The menu bar icons are a cat's head in five moods (catMood), drawn to 32
// pixels (16 points at 2x) of black on transparent by
// scripts/gen-tray-icon.swift. IconIsTemplate has macOS tint them for the
// menu bar, so one file fits a light and a dark menu bar alike.
var (
	//go:embed icon/tray.png
	trayIconPNG []byte
	//go:embed icon/tray-tired.png
	trayTiredIconPNG []byte
	//go:embed icon/tray-weary.png
	trayWearyIconPNG []byte
	//go:embed icon/tray-exhausted.png
	trayExhaustedIconPNG []byte
	//go:embed icon/tray-break.png
	trayBreakIconPNG []byte
)

// trayIcon is the menu bar icon for a mood.
func trayIcon(mood catMood) []byte {
	switch mood {
	case moodTired:
		return trayTiredIconPNG
	case moodWeary:
		return trayWearyIconPNG
	case moodExhausted:
		return trayExhaustedIconPNG
	case moodAsleep:
		return trayBreakIconPNG
	}
	return trayIconPNG
}
