module github.com/Robinbinu/patchbay

go 1.27

// The menu-bar (tray) build pulls in fyne.io/systray; it is only required for
// the `tray` build tag. The core proxy and CLI build with the standard library
// alone. Run `go mod tidy` after building with `-tags tray` to populate this.

require (
	fyne.io/systray v1.11.0
	golang.org/x/sys v0.15.0
)

require github.com/godbus/dbus/v5 v5.1.0 // indirect
