//go:build tray

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"fyne.io/systray"

	"github.com/Robinbinu/patchbay/internal/auth"
	"github.com/Robinbinu/patchbay/internal/autostart"
	"github.com/Robinbinu/patchbay/internal/config"
	"github.com/Robinbinu/patchbay/internal/update"
)

// Links in the menu's footer.
const (
	repoURL     = "https://github.com/Robinbinu/patchbay"
	authorURL   = "https://github.com/Robinbinu"
	linkedinURL = "https://www.linkedin.com/in/michaelrobink"
	releasesURL = repoURL + "/releases"
)

// updateEvery is how often the menu looks for a new release in the background.
const updateEvery = 24 * time.Hour

// updateResult is one finished release check; manual checks report every
// outcome, background ones only a newer release.
type updateResult struct {
	release update.Release
	newer   bool
	err     error
	manual  bool
}

// runUI runs the menu-bar event loop on the main goroutine. On macOS the Cocoa
// loop systray drives must own the main thread, so the caller runs the HTTP
// server on a background goroutine and calls this last. systray.Run blocks
// until the user quits. If the server can't start or stops on its own (say its
// port is taken), the icon shows it instead of the app vanishing, and the menu
// can start it again.
func runUI(cfg *config.Config, mgr *auth.Manager, runner *proxyRunner, moved string, startErr error) {
	systray.Run(func() { onReady(cfg, mgr, runner, moved, startErr) }, func() { os.Exit(0) })
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

// portInput is what the user typed into the port dialog.
type portInput struct {
	text string
	ok   bool
	err  error
}

func onReady(cfg *config.Config, mgr *auth.Manager, runner *proxyRunner, moved string, startErr error) {
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

	// ---- header: status + how to connect ----------------------------------
	header := systray.AddMenuItem("Patchbay "+version, "")
	header.Disable()
	status := systray.AddMenuItem("", "")
	status.Disable()
	connect := systray.AddMenuItem("Connect a Tool", "Base URLs and the local API key for your tools")
	copyOpenAI := connect.AddSubMenuItem("", "")
	copyAnthropic := connect.AddSubMenuItem("", "")
	copyKey := connect.AddSubMenuItem("Copy API Key", "The local pby-… key every tool uses")
	rotate := connect.AddSubMenuItem("Rotate API Key", "Issue a new key; existing tools must update")
	power := systray.AddMenuItem("Stop Proxy", "Stop or start serving; the menu bar stays")
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
		pm.login = pm.parent.AddSubMenuItem("Log In…", "Run the sign-in flow")
		pm.logout = pm.parent.AddSubMenuItem("Log Out", "Forget stored credentials")
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
	atLogin := systray.AddMenuItemCheckbox("Launch at Login", "Start Patchbay when you log in", autostart.Enabled())
	portItem := systray.AddMenuItem("", "Change the port Patchbay listens on")
	checkUpdate := systray.AddMenuItem("Check for Updates…", "Look for a newer release on GitHub")
	showAddr := func() {
		addr := cfg.ListenAddr()
		copyOpenAI.SetTitle(fmt.Sprintf("Copy OpenAI Base URL  (http://%s/v1)", addr))
		copyAnthropic.SetTitle(fmt.Sprintf("Copy Anthropic Base URL  (http://%s)", addr))
		portItem.SetTitle(fmt.Sprintf("Port: %d…", portOf(addr)))
	}
	showAddr()
	systray.AddSeparator()
	about := systray.AddMenuItem("View on GitHub", repoURL)
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
			state, account, expiry, action, needLogin := describe(s)
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
			pm.parent.SetTitle(s.Label + action)
			if action == "" {
				pm.parent.Check() // signed in and usable
			} else {
				pm.parent.Uncheck()
			}
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
			status.SetTitle("Proxy is off")
		case proxyFail != nil:
			status.SetTitle("Proxy stopped: " + proxyFail.Error())
		default:
			line := "Running on " + cfg.ListenAddr()
			if moved != "" {
				line += fmt.Sprintf("  (port %d was taken)", portOf(moved))
			}
			status.SetTitle(line)
		}
		if runner.Running() {
			power.SetTitle("Stop Proxy")
		} else {
			power.SetTitle("Start Proxy")
		}
		setState(stateFor(proxyFail, stopped, warn))
	}
	refresh()

	portInputs := make(chan portInput, 1)
	// changePort moves the proxy to a port the user typed, keeping it off if
	// they had turned it off.
	changePort := func(in portInput) {
		showAddr()
		switch {
		case in.err != nil:
			fmt.Fprintln(os.Stderr, "error: port dialog:", in.err)
			portItem.SetTitle("Port: " + in.err.Error())
			return
		case !in.ok:
			return
		}
		port, err := parsePort(in.text)
		if err != nil {
			portItem.SetTitle(fmt.Sprintf("Port: %d… (%s)", portOf(cfg.ListenAddr()), err))
			return
		}
		addr := withPort(cfg.ListenAddr(), port)
		if addr == cfg.ListenAddr() {
			return
		}
		if !portFree(addr) {
			portItem.SetTitle(fmt.Sprintf("Port: %d… (%d is in use)", portOf(cfg.ListenAddr()), port))
			return
		}
		wasRunning := runner.Running()
		runner.Stop()
		runner.SetAddr(addr)
		cfg.SetListen(addr)
		if err := cfg.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "error: saving port:", err)
		}
		moved = ""
		proxyFail = nil
		if wasRunning || !stopped {
			if proxyFail = runner.Start(); proxyFail != nil {
				fmt.Fprintln(os.Stderr, "error:", proxyFail)
			}
		}
		showAddr()
		refresh()
	}

	updates := make(chan updateResult, 1)
	var available *update.Release
	checking := false
	startCheck := func(manual bool) {
		if checking {
			return
		}
		checking = true
		if manual {
			checkUpdate.SetTitle("Checking for Updates…")
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			r, newer, err := update.Check(ctx, update.DefaultClient, version)
			updates <- updateResult{release: r, newer: newer, err: err, manual: manual}
		}()
	}
	// Development builds have no version to compare; don't check in the background.
	devBuild := version == "dev"
	updateTicker := time.NewTicker(updateEvery)
	if !devBuild {
		startCheck(false)
	}

	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				refresh()
			case <-updateTicker.C:
				if !devBuild && available == nil {
					startCheck(false)
				}
			case <-checkUpdate.ClickedCh:
				switch {
				case available != nil:
					_ = auth.OpenBrowser(available.URL)
				case devBuild:
					checkUpdate.SetTitle("Development Build: See All Releases")
					_ = auth.OpenBrowser(releasesURL)
				default:
					startCheck(true)
				}
			case res := <-updates:
				checking = false
				switch {
				case res.newer:
					available = &res.release
					checkUpdate.SetTitle("Update Available: " + res.release.Tag + " — Download")
				case !res.manual:
					// Background checks stay quiet unless there is something new.
				case res.err != nil:
					fmt.Fprintln(os.Stderr, "error: update check:", res.err)
					checkUpdate.SetTitle("Update Check Failed — Try Again")
				default:
					checkUpdate.SetTitle("Patchbay is up to date (" + version + ")")
				}
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
			case <-copyOpenAI.ClickedCh:
				_ = clipboardCopy(fmt.Sprintf("http://%s/v1", cfg.ListenAddr()))
			case <-copyAnthropic.ClickedCh:
				_ = clipboardCopy(fmt.Sprintf("http://%s", cfg.ListenAddr()))
			case <-copyKey.ClickedCh:
				_ = clipboardCopy(cfg.LocalKey())
			case <-rotate.ClickedCh:
				cfg.RotateLocalKey()
				_ = cfg.Save()
			case <-portItem.ClickedCh:
				cur := strconv.Itoa(portOf(cfg.ListenAddr()))
				go func() {
					text, ok, err := promptText("Patchbay",
						"Port for Patchbay (1024–65535). Tools must use the new base URL afterwards.", cur)
					portInputs <- portInput{text, ok, err}
				}()
			case in := <-portInputs:
				changePort(in)
			case <-atLogin.ClickedCh:
				if err := autostart.Set(!atLogin.Checked()); err != nil {
					fmt.Fprintln(os.Stderr, "error: launch at login:", err)
					atLogin.SetTitle("Launch at Login (" + err.Error() + ")")
				} else {
					atLogin.SetTitle("Launch at Login")
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
				updateTicker.Stop()
				systray.Quit()
				return
			}
		}
	}()
}

// describe turns a status into display strings: state line, account line,
// expiry line, the action the provider's title asks for ("" when it is ready
// to use, which the menu shows as a checkmark), and whether to badge the icon.
func describe(s auth.Status) (state, account, expiry, action string, needLogin bool) {
	switch {
	case s.Local && s.SignedIn:
		return "running", s.Address, "", "", false
	case s.Local:
		// A local server that isn't running is normal, not a problem to badge.
		return "not running", s.Address, "", "  —  Not Running", false
	case s.NeedsKey:
		return "no API key", "", "", "  —  Add API Key", false
	case !s.SignedIn:
		return "signed out", "", "", "  —  Sign In", true
	case s.GrantExpired:
		return "login expired — sign in again", acct(s), "", "  —  Sign In Again", true
	case s.Expired:
		return "token expired (auto-refreshing)", acct(s), expiryLine(s), "", false
	default:
		return "signed in", acct(s), expiryLine(s), "", false
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
