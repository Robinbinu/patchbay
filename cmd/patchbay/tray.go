//go:build tray

package main

import (
	"context"
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

// providerMenu is the set of items that make up one provider's submenu.
type providerMenu struct {
	parent  *systray.MenuItem
	status  *systray.MenuItem
	account *systray.MenuItem
	expiry  *systray.MenuItem
	login   *systray.MenuItem
	logout  *systray.MenuItem
}

func onReady(cfg *config.Config, mgr *auth.Manager) {
	icon := trayIconPNG()
	systray.SetTemplateIcon(icon, icon)
	systray.SetTitle("")
	systray.SetTooltip("Patchbay — model proxy")

	// ---- header: endpoint + key -------------------------------------------
	header := systray.AddMenuItem("Patchbay", "")
	header.Disable()
	ep := systray.AddMenuItem(fmt.Sprintf("Endpoint  http://%s", cfg.Listen), "OpenAI base is /v1; Anthropic base is the root")
	copyEP := ep.AddSubMenuItem("Copy OpenAI base URL", "")
	copyKey := ep.AddSubMenuItem("Copy local API key", "")
	rotate := ep.AddSubMenuItem("Rotate local API key", "Issue a new key; existing clients must update")
	systray.AddSeparator()

	// ---- providers ---------------------------------------------------------
	accounts := systray.AddMenuItem("Accounts", "")
	accounts.Disable()
	menus := map[string]*providerMenu{}
	for _, p := range cfg.Providers {
		pm := &providerMenu{parent: systray.AddMenuItem(p.Label, "")}
		pm.status = pm.parent.AddSubMenuItem("", "")
		pm.status.Disable()
		pm.account = pm.parent.AddSubMenuItem("", "")
		pm.account.Disable()
		pm.expiry = pm.parent.AddSubMenuItem("", "")
		pm.expiry.Disable()
		pm.login = pm.parent.AddSubMenuItem("Log in…", "Run the sign-in flow")
		pm.logout = pm.parent.AddSubMenuItem("Log out", "Forget stored credentials")
		menus[p.ID] = pm

		prov := p
		go func() {
			for range pm.login.ClickedCh {
				go func() {
					_, err := mgr.Login(context.Background(), prov)
					if err != nil {
						pm.status.SetTitle("Status: sign-in failed")
					}
				}()
			}
		}()
		go func() {
			for range pm.logout.ClickedCh {
				_ = mgr.Store().Delete(prov.ID)
			}
		}()
	}

	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit Patchbay", "Stop the proxy and the menu bar")

	refresh := func() {
		warn := false
		for _, s := range mgr.Statuses(cfg) {
			pm := menus[s.ID]
			if pm == nil {
				continue
			}
			state, account, expiry, badge, needLogin := describe(s)
			pm.status.SetTitle("Status: " + state)
			if account == "" {
				pm.account.Hide()
			} else {
				pm.account.SetTitle(account)
				pm.account.Show()
			}
			if expiry == "" {
				pm.expiry.Hide()
			} else {
				pm.expiry.SetTitle(expiry)
				pm.expiry.Show()
			}
			pm.parent.SetTitle(s.Label + badge)
			// Key providers don't log in interactively; hide the action.
			if config.OAuthKind(s.Kind) {
				pm.login.Show()
				pm.logout.Show()
			} else {
				pm.login.Hide()
				pm.logout.Hide()
			}
			if needLogin {
				warn = true
			}
		}
		if warn {
			systray.SetTitle(" ⚠")
		} else {
			systray.SetTitle("")
		}
	}
	refresh()

	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				refresh()
			case <-copyEP.ClickedCh:
				_ = clipboardCopy(fmt.Sprintf("http://%s/v1", cfg.Listen))
			case <-copyKey.ClickedCh:
				_ = clipboardCopy(cfg.LocalAPIKey)
			case <-rotate.ClickedCh:
				cfg.RotateLocalKey()
				_ = cfg.Save()
			case <-quit.ClickedCh:
				ticker.Stop()
				systray.Quit()
				return
			}
		}
	}()
}

// describe turns a status into display strings: state line, account line,
// expiry line, a parent-title badge, and whether attention is needed.
func describe(s auth.Status) (state, account, expiry, badge string, needLogin bool) {
	switch {
	case s.NeedsKey:
		return "no API key", "", "", "  —", false
	case !s.SignedIn:
		return "signed out", "", "", "  ○", true
	case s.GrantExpired:
		return "login expired — sign in again", acct(s), "", "  ⚠", true
	case s.Expired:
		return "token expired (auto-refreshing)", acct(s), expiryLine(s), "  ●", false
	default:
		return "signed in", acct(s), expiryLine(s), "  ●", false
	}
}

func acct(s auth.Status) string {
	who := s.Email
	if who == "" {
		who = "signed in"
	}
	if s.Plan != "" {
		who += "  ·  " + s.Plan
	}
	return who
}

func expiryLine(s auth.Status) string {
	t := s.GrantExpiry
	label := "Login expires"
	if t.IsZero() {
		t = s.AccessExpiry
		label = "Token expires"
	}
	if t.IsZero() {
		return "" // durable key: no expiry
	}
	d := time.Until(t)
	switch {
	case d <= 0:
		return label + ": expired"
	case d < time.Hour:
		return fmt.Sprintf("%s in %dm", label, int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%s in %dh", label, int(d.Hours()))
	default:
		return fmt.Sprintf("%s in %dd", label, int(d.Hours()/24))
	}
}
