//go:build !tray

package main

import (
	"github.com/robin/patchbay/internal/auth"
	"github.com/robin/patchbay/internal/config"
)

// runUI blocks forever in the default build; the HTTP server runs on its own
// goroutine. Build with `-tags tray` for the menu-bar UI (fyne.io/systray).
func runUI(_ *config.Config, _ *auth.Manager) {
	select {}
}
