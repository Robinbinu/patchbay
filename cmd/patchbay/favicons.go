package main

import (
	"embed"
	"net/url"
	"strings"

	"github.com/Robinbinu/patchbay/internal/config"
	"github.com/Robinbinu/patchbay/internal/providers"
)

// Provider favicons for the menu, 32px so they stay sharp at 16pt on Retina.
// They are embedded rather than fetched: several providers block non-browser
// requests for their favicon. Each is the provider's mark alone, black on
// transparent, so macOS can tint it as a template image like the menu-bar icon.
//
//go:embed favicons/*.png
var faviconFS embed.FS

// providerFavicon returns a provider's favicon as PNG, or nil when Patchbay has
// none for it (a custom OpenAI-compatible host, say).
func providerFavicon(p config.Provider) []byte {
	name := faviconName(p)
	if name == "" {
		return nil
	}
	b, err := faviconFS.ReadFile("favicons/" + name + ".png")
	if err != nil {
		panic(err) // the embedded set is fixed at build time
	}
	return b
}

// faviconName picks the favicon by provider kind, or for API-key kinds by the
// upstream host, since one kind can point at many services.
func faviconName(p config.Provider) string {
	switch p.Kind {
	case config.KindCodexOAuth:
		return "openai"
	case config.KindAnthropicOAuth:
		return "claude"
	case config.KindXAIOAuth:
		return "xai"
	case config.KindOpenRouterOAuth:
		return "openrouter"
	}
	u, err := url.Parse(providers.BaseURL(p))
	if err != nil {
		return ""
	}
	host := u.Hostname()
	for domain, name := range map[string]string{
		"openai.com":    "openai",
		"anthropic.com": "anthropic",
		"x.ai":          "xai",
		"openrouter.ai": "openrouter",
	} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return name
		}
	}
	return ""
}
