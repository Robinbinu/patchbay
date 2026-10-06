package main

import (
	"testing"

	"github.com/Robinbinu/patchbay/internal/config"
)

func TestFaviconFor(t *testing.T) {
	cases := []struct {
		p    config.Provider
		want string
	}{
		{config.Provider{Kind: config.KindCodexOAuth}, "openai"},
		{config.Provider{Kind: config.KindAnthropicOAuth}, "claude"},
		{config.Provider{Kind: config.KindAnthropicKey}, "anthropic"},
		{config.Provider{Kind: config.KindXAIOAuth}, "xai"},
		{config.Provider{Kind: config.KindOpenRouterOAuth}, "openrouter"},
		{config.Provider{Kind: config.KindOpenAIKey}, "openai"}, // default base is api.openai.com
		{config.Provider{Kind: config.KindOpenAIKey, BaseURL: "https://openrouter.ai/api/v1"}, "openrouter"},
		{config.Provider{Kind: config.KindOpenAIKey, BaseURL: "https://api.x.ai/v1"}, "xai"},
		{config.Provider{Kind: config.KindOpenAIKey, BaseURL: "https://api.deepseek.com/v1"}, ""},
		{config.Provider{Kind: config.KindOpenAIKey, BaseURL: "https://notopenai.com/v1"}, ""}, // suffix match is per label
	}
	for _, c := range cases {
		if got := faviconName(c.p); got != c.want {
			t.Errorf("faviconName(%s %q) = %q, want %q", c.p.Kind, c.p.BaseURL, got, c.want)
		}
	}
}

func TestEmbeddedFaviconsAre32px(t *testing.T) {
	for _, name := range []string{"openai", "claude", "anthropic", "xai", "openrouter"} {
		b, err := faviconFS.ReadFile("favicons/" + name + ".png")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if s := decodePNG(t, b).Bounds().Size(); s.X != 32 || s.Y != 32 {
			t.Errorf("%s favicon is %v, want 32x32 (16pt menu icon @2x)", name, s)
		}
	}
	if providerFavicon(config.Provider{Kind: config.KindOpenAIKey, BaseURL: "https://api.deepseek.com"}) != nil {
		t.Errorf("unknown host should have no favicon")
	}
}
