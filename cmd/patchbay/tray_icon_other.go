//go:build tray && !windows

package main

// macOS tints template images for the menu bar's appearance itself.
const templateIcon = true

func trayIcon(st trayState) []byte { return templateIconPNG(st) }

func menuIcon(png []byte) []byte { return png }
