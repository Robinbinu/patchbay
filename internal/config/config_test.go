package config

import (
	"os"
	"path/filepath"
	"testing"
)

func ids(c *Config) map[string]bool {
	out := map[string]bool{}
	for _, p := range c.Providers {
		out[p.ID] = true
	}
	return out
}

func TestRemovedSeededProviderStaysRemoved(t *testing.T) {
	setHome(t, t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ids(c)["claude"] {
		t.Fatal("fresh config should include claude")
	}
	kept := c.Providers[:0]
	for _, p := range c.Providers {
		if p.ID != "claude" {
			kept = append(kept, p)
		}
	}
	c.Providers = kept
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if ids(again)["claude"] {
		t.Fatal("removed seeded provider came back on reload")
	}
}

// A config written before Seeded existed gains defaults it lacks once, and a
// removal after that sticks.
func TestLegacyConfigBackfillsOnce(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	dir := filepath.Join(home, ".patchbay")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"listen":"127.0.0.1:8787","local_api_key":"pby-x","providers":[{"id":"codex","kind":"codex-oauth","label":"ChatGPT (Codex)","enabled":true}]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"codex", "claude", "grok", "openrouter"} {
		if !ids(c)[id] {
			t.Errorf("legacy config missing %q after backfill", id)
		}
	}
	c.Providers = c.Providers[:1] // keep only codex
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Providers) != 1 {
		t.Fatalf("providers after removal and reload = %v", ids(again))
	}
}

// setHome points the user's home at dir for the test: HOME on macOS and
// Linux, USERPROFILE on Windows (where os.UserHomeDir ignores HOME).
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
