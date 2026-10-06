//go:build !tray

package main

import (
	"github.com/robin/patchbay/internal/auth"
	"github.com/robin/patchbay/internal/config"
)

// startTray is a no-op in the default build. Build with `-tags tray` for the
// menu-bar UI (pulls in fyne.io/systray).
func startTray(_ *config.Config, _ *auth.Manager) {}
