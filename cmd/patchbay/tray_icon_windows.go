//go:build tray && windows

package main

import "golang.org/x/sys/windows/registry"

// Windows has no template images; the icon is inked for the taskbar theme.
const templateIcon = false

func trayIcon(st trayState) []byte { return windowsIconICO(st, lightTaskbar()) }

// menuIcon wraps a 32px PNG for a menu item; Windows loads menu icons from ICO.
func menuIcon(png []byte) []byte { return encodeICO([]int{32}, [][]byte{png}) }

// lightTaskbar reports whether the taskbar uses the light theme. The system
// (taskbar) theme is separate from the app theme; it defaults to dark.
func lightTaskbar() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("SystemUsesLightTheme")
	return err == nil && v == 1
}
