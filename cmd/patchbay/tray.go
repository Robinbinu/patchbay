//go:build tray

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"fyne.io/systray"

	"github.com/Robinbinu/patchbay/internal/auth"
	"github.com/Robinbinu/patchbay/internal/autostart"
	"github.com/Robinbinu/patchbay/internal/config"
)

// Links in the menu's footer.
const (
	repoURL     = "https://github.com/Robinbinu/patchbay"
	authorURL   = "https://github.com/Robinbinu"
	linkedinURL = "https://www.linkedin.com/in/michaelrobink"
)

// runUI runs the menu-bar event loop on the main goroutine. On macOS the Cocoa
// loop systray drives must own the main thread, so the caller runs the HTTP
// server on a background goroutine and calls this last. systray.Run blocks
// until the user quits. If the server can't start or stops on its own (say its
// port is taken), the icon shows it instead of the app vanishing, and the menu
// can start it again.
func runUI(cfg *config.Config, mgr *auth.Manager, runner *proxyRunner, startErr error) {
	systray.Run(func() { onReady(cfg, mgr, runner, startErr) }, func() { os.Exit(0) })
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

func onReady(cfg *config.Config, mgr *auth.Manager, runner *proxyRunner, startErr error) {
	var shown []byte
	setState := func(st trayState) {
		if icon := trayIcon(st); !bytes.Equal(icon, shown) {
			shown = icon
			if templateIcon {
				systray.SetTemplateIcon(icon, icon)
			} else {
				systray.SetIcon(icon)
			}
		}
		switch st {
		case stateStopped:
			systray.SetTooltip("Patchbay — proxy off")
		case stateProxyDown:
			systray.SetTooltip("Patchbay — proxy stopped")
		case stateNeedsLogin:
			systray.SetTooltip("Patchbay — a provider needs you to sign in")
		default:
			systray.SetTooltip("Patchbay — model proxy")
		}
	}

	// ---- header: endpoint + key -------------------------------------------
	header := systray.AddMenuItem("Patchbay", "")
	header.Disable()
	down := systray.AddMenuItem("", "Patchbay could not serve on its address")
	down.Disable()
	down.Hide()
	ep := systray.AddMenuItem(fmt.Sprintf("Endpoint  http://%s", cfg.Listen), "OpenAI base is /v1; Anthropic base is the root")
	copyEP := ep.AddSubMenuItem("Copy OpenAI base URL", "")
	copyKey := ep.AddSubMenuItem("Copy local API key", "")
	rotate := ep.AddSubMenuItem("Rotate local API key", "Issue a new key; existing clients must update")
	power := systray.AddMenuItem("Stop proxy", "Stop or start serving; the menu bar stays")
	atLogin := systray.AddMenuItemCheckbox("Launch at login", "Start Patchbay when you log in", autostart.Enabled())
	systray.AddSeparator()

	// ---- providers ---------------------------------------------------------
	accounts := systray.AddMenuItem("Accounts", "")
	accounts.Disable()
	menus := map[string]*providerMenu{}
	for _, p := range cfg.Providers {
		pm := &providerMenu{parent: systray.AddMenuItem(p.Label, "")}
		if fav := providerFavicon(p); fav != nil {
			if templateIcon {
				pm.parent.SetTemplateIcon(fav, fav)
			} else {
				pm.parent.SetIcon(menuIcon(fav))
			}
		}
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
	about := systray.AddMenuItem("Patchbay "+version+" on GitHub", repoURL)
	author := systray.AddMenuItem("Made by Robinbinu", authorURL)
	linkedin := systray.AddMenuItem("Connect on LinkedIn", linkedinURL)
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit Patchbay", "Stop the proxy and the menu bar")

	proxyFail := startErr
	stopped := false
	if startErr != nil {
		fmt.Fprintln(os.Stderr, "error:", startErr)
	}
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
		switch {
		case stopped:
			down.SetTitle("Proxy is off")
			down.Show()
		case proxyFail != nil:
			down.SetTitle("Proxy stopped: " + proxyFail.Error())
			down.Show()
		default:
			down.Hide()
		}
		if runner.Running() {
			power.SetTitle("Stop proxy")
		} else {
			power.SetTitle("Start proxy")
		}
		setState(stateFor(proxyFail, stopped, warn))
	}
	refresh()

	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				refresh()
			case err := <-runner.Failed():
				fmt.Fprintln(os.Stderr, "error:", err)
				proxyFail = err
				refresh()
			case <-power.ClickedCh:
				if runner.Running() {
					runner.Stop()
					stopped, proxyFail = true, nil
				} else {
					stopped = false
					if proxyFail = runner.Start(); proxyFail != nil {
						fmt.Fprintln(os.Stderr, "error:", proxyFail)
					}
				}
				refresh()
			case <-copyEP.ClickedCh:
				_ = clipboardCopy(fmt.Sprintf("http://%s/v1", cfg.Listen))
			case <-copyKey.ClickedCh:
				_ = clipboardCopy(cfg.LocalAPIKey)
			case <-rotate.ClickedCh:
				cfg.RotateLocalKey()
				_ = cfg.Save()
			case <-atLogin.ClickedCh:
				if err := autostart.Set(!atLogin.Checked()); err != nil {
					fmt.Fprintln(os.Stderr, "error: launch at login:", err)
					atLogin.SetTitle("Launch at login (" + err.Error() + ")")
				} else {
					atLogin.SetTitle("Launch at login")
				}
				if autostart.Enabled() {
					atLogin.Check()
				} else {
					atLogin.Uncheck()
				}
			case <-about.ClickedCh:
				_ = auth.OpenBrowser(repoURL)
			case <-author.ClickedCh:
				_ = auth.OpenBrowser(authorURL)
			case <-linkedin.ClickedCh:
				_ = auth.OpenBrowser(linkedinURL)
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
