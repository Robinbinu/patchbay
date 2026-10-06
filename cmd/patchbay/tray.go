//go:build tray

package main

import (
	"fmt"
	"os"
	"time"

	"fyne.io/systray"

	"github.com/robin/patchbay/internal/auth"
	"github.com/robin/patchbay/internal/config"
)

// runUI runs the menu-bar event loop on the main goroutine. On macOS the Cocoa
// loop systray drives must own the main thread, so the caller runs the HTTP
// server on a background goroutine and calls this last. systray.Run blocks
// until the user quits.
func runUI(cfg *config.Config, mgr *auth.Manager) {
	systray.Run(func() { onReady(cfg, mgr) }, func() { os.Exit(0) })
}

func onReady(cfg *config.Config, mgr *auth.Manager) {
	systray.SetTitle("⌁")
	systray.SetTooltip("Patchbay — model proxy")

	endpoint := systray.AddMenuItem(fmt.Sprintf("Endpoint: http://%s/v1", cfg.Listen), "OpenAI-compatible base URL")
	endpoint.Disable()
	copyKey := systray.AddMenuItem("Copy local API key", "Copy the proxy's local API key")
	systray.AddSeparator()

	// One status line per provider, refreshed on a ticker.
	items := map[string]*systray.MenuItem{}
	logins := map[string]*systray.MenuItem{}
	for _, p := range cfg.Providers {
		it := systray.AddMenuItem(p.Label, "")
		it.Disable()
		items[p.ID] = it
		li := systray.AddMenuItem("    Log in again", "Re-run the OAuth sign-in for "+p.Label)
		li.Hide()
		logins[p.ID] = li
		pid := p.ID
		pp := p
		go func() {
			for range li.ClickedCh {
				_, _ = mgr.Login(contextBackground(), pp)
				_ = pid
			}
		}()
	}

	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit Patchbay", "Stop the proxy")

	refresh := func() {
		warn := false
		for _, s := range mgr.Statuses(cfg) {
			label, needLogin := trayLine(s)
			if it := items[s.ID]; it != nil {
				it.SetTitle(label)
			}
			if li := logins[s.ID]; li != nil {
				if needLogin {
					li.Show()
				} else {
					li.Hide()
				}
			}
			if needLogin {
				warn = true
			}
		}
		if warn {
			systray.SetTitle("⚠")
		} else {
			systray.SetTitle("⌁")
		}
	}
	refresh()

	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				refresh()
			case <-copyKey.ClickedCh:
				_ = clipboardCopy(cfg.LocalAPIKey)
			case <-quit.ClickedCh:
				ticker.Stop()
				systray.Quit()
				return
			}
		}
	}()
}

func trayLine(s auth.Status) (label string, needLogin bool) {
	switch {
	case s.NeedsKey:
		return s.Label + " — no API key", false
	case !s.SignedIn:
		return s.Label + " — signed out", true
	case s.GrantExpired:
		return s.Label + " — login expired", true
	case s.Expired:
		return s.Label + " — token expired (refreshing)", false
	}
	who := s.Email
	if who == "" {
		who = "signed in"
	}
	exp := s.GrantExpiry
	if exp.IsZero() {
		exp = s.AccessExpiry
	}
	if !exp.IsZero() {
		d := time.Until(exp)
		switch {
		case d <= 0:
			return s.Label + " — " + who + " (expired)", true
		case d < 48*time.Hour:
			return fmt.Sprintf("%s — %s (expires in %dh)", s.Label, who, int(d.Hours())), false
		default:
			return fmt.Sprintf("%s — %s (expires in %dd)", s.Label, who, int(d.Hours()/24)), false
		}
	}
	return s.Label + " — " + who, false
}
