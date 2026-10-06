// Package config owns Patchbay's on-disk state: the proxy listen address, the
// locally generated API key that HTTP clients must present, and the list of
// configured providers. Everything lives under ~/.patchbay with 0700/0600
// permissions.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Provider kinds understood by the proxy router.
const (
	KindCodexOAuth      = "codex-oauth"      // ChatGPT plan, OAuth, Responses API
	KindAnthropicOAuth  = "anthropic-oauth"  // Claude Pro/Max, OAuth, Messages API
	KindAnthropicKey    = "anthropic-key"    // Anthropic Console API key, Messages API
	KindOpenAIKey       = "openai-key"       // OpenAI-compatible API key, Chat/Responses
	KindXAIOAuth        = "xai-oauth"        // Grok (SuperGrok/X Premium+), OAuth device-code, Responses API
	KindOpenRouterOAuth = "openrouter-oauth" // OpenRouter, OAuth (PKCE) → durable key, Chat API
)

// OAuthKind reports whether a provider kind signs in interactively (vs. a key).
func OAuthKind(kind string) bool {
	switch kind {
	case KindCodexOAuth, KindAnthropicOAuth, KindXAIOAuth, KindOpenRouterOAuth:
		return true
	}
	return false
}

// Provider is one configured upstream. OAuth kinds draw their token from the
// credential store (keyed by ID); key kinds carry the key inline.
type Provider struct {
	ID      string `json:"id"`                 // stable key, e.g. "codex", "claude", "openai"
	Kind    string `json:"kind"`               // one of the Kind* constants
	Label   string `json:"label"`              // human name for the menu
	BaseURL string `json:"base_url,omitempty"` // upstream base; defaults applied per kind
	APIKey  string `json:"api_key,omitempty"`  // only for *-key kinds
	Enabled bool   `json:"enabled"`
}

// Config is the whole persisted document.
type Config struct {
	Listen      string     `json:"listen"`
	LocalAPIKey string     `json:"local_api_key"`
	Providers   []Provider `json:"providers"`

	mu   sync.Mutex `json:"-"`
	path string     `json:"-"`
}

// Dir is ~/.patchbay.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".patchbay"), nil
}

func ensureDir() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

func newLocalKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "pby-" + hex.EncodeToString(b)
}

// Default seeds the two OAuth providers Patchbay ships with. Key-based
// providers are added by the user via `patchbay provider add`.
func Default() *Config {
	return &Config{
		Listen:      "127.0.0.1:8787",
		LocalAPIKey: newLocalKey(),
		Providers: []Provider{
			{ID: "codex", Kind: KindCodexOAuth, Label: "ChatGPT (Codex)", Enabled: true},
			{ID: "claude", Kind: KindAnthropicOAuth, Label: "Claude (Pro/Max)", Enabled: true},
			{ID: "grok", Kind: KindXAIOAuth, Label: "Grok (xAI)", Enabled: true},
			{ID: "openrouter", Kind: KindOpenRouterOAuth, Label: "OpenRouter", Enabled: true},
		},
	}
}

// Load reads ~/.patchbay/config.json, creating a default document on first run.
func Load() (*Config, error) {
	d, err := ensureDir()
	if err != nil {
		return nil, err
	}
	p := filepath.Join(d, "config.json")
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		c := Default()
		c.path = p
		return c, c.Save()
	}
	if err != nil {
		return nil, err
	}
	c := &Config{}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	c.path = p
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8787"
	}
	if c.LocalAPIKey == "" {
		c.LocalAPIKey = newLocalKey()
	}
	// Backfill seeded providers added in newer versions so an existing config
	// gains them (e.g. grok, openrouter) without losing the user's own entries.
	if c.ensureSeeded() {
		_ = c.Save()
	}
	return c, nil
}

// ensureSeeded adds any default provider missing from the loaded config,
// matched by ID. Returns whether anything changed.
func (c *Config) ensureSeeded() bool {
	changed := false
	for _, def := range Default().Providers {
		if _, ok := c.Provider(def.ID); !ok {
			c.Providers = append(c.Providers, def)
			changed = true
		}
	}
	return changed
}

// Save atomically writes the document back.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		d, err := ensureDir()
		if err != nil {
			return err
		}
		c.path = filepath.Join(d, "config.json")
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Provider returns the configured provider with the given ID.
func (c *Config) Provider(id string) (*Provider, bool) {
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			return &c.Providers[i], true
		}
	}
	return nil, false
}

// Upsert replaces a provider with the same ID, or appends it.
func (c *Config) Upsert(p Provider) {
	for i := range c.Providers {
		if c.Providers[i].ID == p.ID {
			c.Providers[i] = p
			return
		}
	}
	c.Providers = append(c.Providers, p)
}

// LocalKey returns the local API key clients must present. The proxy reads it
// on every request while the menu may rotate it, so access goes through c.mu.
func (c *Config) LocalKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.LocalAPIKey
}

// RotateLocalKey generates a fresh local API key and returns it.
func (c *Config) RotateLocalKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LocalAPIKey = newLocalKey()
	return c.LocalAPIKey
}
