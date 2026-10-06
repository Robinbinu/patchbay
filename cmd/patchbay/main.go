// Command patchbay is a local, OS-agnostic proxy that fronts multiple model
// providers behind one OpenAI- and Anthropic-compatible endpoint. It signs in
// to OAuth providers (ChatGPT/Codex, Claude Pro/Max) the way omp does, stores
// and refreshes the tokens, and reports sign-in expiry.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/Robinbinu/patchbay/internal/auth"
	"github.com/Robinbinu/patchbay/internal/config"
	"github.com/Robinbinu/patchbay/internal/proxy"
)

// version is stamped at release build time with -ldflags "-X main.version=…".
var version = "dev"

// appBuild is stamped to "1" for the double-clickable macOS .app and Windows
// GUI builds, which the OS launches with no arguments.
var appBuild = ""

func main() {
	args := commandArgs(os.Args[1:], appBuild == "1")
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println("patchbay", version)
		return
	}
	cfg, err := config.Load()
	check(err)
	store, err := auth.OpenStore()
	check(err)
	mgr := auth.NewManager(store)

	cmd := args[0]
	args = args[1:]
	switch cmd {
	case "serve":
		runServe(cfg, mgr)
	case "login":
		runLogin(cfg, mgr, args)
	case "logout":
		runLogout(cfg, mgr, args)
	case "status":
		runStatus(cfg, mgr)
	case "provider":
		runProvider(cfg, args)
	case "key":
		runKey(cfg, args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func runServe(cfg *config.Config, mgr *auth.Manager) {
	srv := proxy.New(cfg, mgr)
	fmt.Printf("Patchbay listening on http://%s\n", cfg.Listen)
	fmt.Printf("  Local API key: %s\n", cfg.LocalKey())
	fmt.Printf("  OpenAI base:    http://%s/v1\n", cfg.Listen)
	fmt.Printf("  Anthropic base: http://%s\n", cfg.Listen)
	fmt.Println("  Endpoints: /v1/models  /v1/chat/completions  /v1/responses  /v1/messages")
	// The HTTP server runs on a background goroutine so the main goroutine is
	// free for the UI loop: on macOS the menu-bar (Cocoa) event loop must own
	// the main thread. Without -tags tray, runUI exits when the server stops.
	runner := newProxyRunner(cfg.Listen, srv.Handler())
	startErr := runner.Start()
	runUI(cfg, mgr, runner, startErr)
}

func runLogin(cfg *config.Config, mgr *auth.Manager, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: patchbay login <provider-id>   (e.g. codex, claude)")
		os.Exit(2)
	}
	p, ok := cfg.Provider(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "no provider %q configured; see `patchbay status`\n", args[0])
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	fmt.Printf("Signing in to %s…\n", p.Label)
	c, err := mgr.Login(ctx, *p)
	check(err)
	who := c.Email
	if who == "" {
		who = c.AccountID
	}
	fmt.Printf("Signed in as %s", who)
	if c.OrgName != "" {
		fmt.Printf(" (%s)", c.OrgName)
	}
	fmt.Println(".")
}

func runLogout(cfg *config.Config, mgr *auth.Manager, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: patchbay logout <provider-id>")
		os.Exit(2)
	}
	check(mgr.Store().Delete(args[0]))
	fmt.Printf("Signed out of %q.\n", args[0])
}

func runStatus(cfg *config.Config, mgr *auth.Manager) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "PROVIDER\tKIND\tSTATE\tACCOUNT\tACCESS EXPIRES\tLOGIN EXPIRES")
	for _, s := range mgr.Statuses(cfg) {
		state := "signed out"
		switch {
		case s.NeedsKey:
			state = "no key"
		case s.GrantExpired:
			state = "re-login needed"
		case s.Expired:
			state = "token expired"
		case s.SignedIn:
			state = "ok"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			s.ID, s.Kind, state, dash(s.Email),
			until(s.AccessExpiry), until(s.GrantExpiry))
	}
	tw.Flush()
}

func runProvider(cfg *config.Config, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: patchbay provider add <id> <kind> [base_url] | patchbay provider key <id> <api-key> | patchbay provider rm <id>")
		os.Exit(2)
	}
	switch args[0] {
	case "add":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: patchbay provider add <id> <kind> [base_url]")
			fmt.Fprintln(os.Stderr, "kinds: "+strings.Join([]string{config.KindOpenAIKey, config.KindAnthropicKey, config.KindCodexOAuth, config.KindAnthropicOAuth, config.KindXAIOAuth, config.KindOpenRouterOAuth}, ", "))
			os.Exit(2)
		}
		p := config.Provider{ID: args[1], Kind: args[2], Label: args[1], Enabled: true}
		if len(args) >= 4 {
			p.BaseURL = args[3]
		}
		cfg.Upsert(p)
		check(cfg.Save())
		fmt.Printf("Added provider %q (%s).\n", p.ID, p.Kind)
	case "key":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: patchbay provider key <id> <api-key>")
			os.Exit(2)
		}
		p, ok := cfg.Provider(args[1])
		if !ok {
			fmt.Fprintf(os.Stderr, "no provider %q\n", args[1])
			os.Exit(1)
		}
		p.APIKey = args[2]
		cfg.Upsert(*p)
		check(cfg.Save())
		fmt.Printf("Stored API key for %q.\n", args[1])
	case "rm":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: patchbay provider rm <id>")
			os.Exit(2)
		}
		kept := cfg.Providers[:0]
		for _, p := range cfg.Providers {
			if p.ID != args[1] {
				kept = append(kept, p)
			}
		}
		cfg.Providers = kept
		check(cfg.Save())
		fmt.Printf("Removed provider %q.\n", args[1])
	default:
		fmt.Fprintf(os.Stderr, "unknown provider subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runKey(cfg *config.Config, args []string) {
	if len(args) >= 1 && args[0] == "rotate" {
		key := cfg.RotateLocalKey()
		check(cfg.Save())
		fmt.Printf("New local API key: %s\n", key)
		return
	}
	fmt.Println(cfg.LocalKey())
}

func usage() {
	fmt.Fprint(os.Stderr, `patchbay — one local endpoint for all your models

Usage:
  patchbay serve                      Start the proxy (and menu bar, if built with -tags tray)
  patchbay login <id>                 Sign in to an OAuth provider (codex, claude, grok, openrouter)
  patchbay logout <id>                Forget an OAuth provider's credentials
  patchbay status                     Show every provider's sign-in and expiry
  patchbay provider add <id> <kind> [base_url]
  patchbay provider key <id> <api-key>
  patchbay provider rm <id>
  patchbay key [rotate]               Print (or rotate) the local API key
  patchbay version                    Print the Patchbay version

Provider kinds:
  codex-oauth      ChatGPT plan (Codex), OAuth
  anthropic-oauth  Claude Pro/Max, OAuth
  xai-oauth        Grok (SuperGrok / X Premium+), OAuth device code
  openrouter-oauth OpenRouter, OAuth
  anthropic-key    Anthropic Console API key
  openai-key       OpenAI-compatible API key (OpenAI, OpenRouter, DeepSeek, …)
`)
}

// commandArgs returns the command line to run. An app build launched with no
// command starts serving. Older macOS appends a -psn_… process serial number
// when launching from Finder, which is not a command.
func commandArgs(args []string, app bool) []string {
	if len(args) > 0 && strings.HasPrefix(args[0], "-psn_") {
		args = args[1:]
	}
	if len(args) == 0 && app {
		return []string{"serve"}
	}
	return args
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func until(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Until(t)
	if d <= 0 {
		return "expired"
	}
	if d < time.Hour {
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("in %dh", int(d.Hours()))
	}
	return fmt.Sprintf("in %dd", int(d.Hours()/24))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

var _ = syscall.SIGTERM
