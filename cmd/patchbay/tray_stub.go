//go:build !tray

package main

import (
	"github.com/robin/patchbay/internal/auth"
	"github.com/robin/patchbay/internal/config"
)

// runUI waits on the HTTP server in the default build and exits if it stops.
// Build with `-tags tray` for the menu-bar UI (fyne.io/systray).
func runUI(_ *config.Config, _ *auth.Manager, proxyErr <-chan error) {
	check(<-proxyErr)
}
