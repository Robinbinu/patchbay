package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robinbinu/patchbay/internal/config"
)

var claude = config.Provider{ID: "claude", Kind: config.KindAnthropicOAuth}

func creds(access, refresh string, valid time.Duration) Credentials {
	return Credentials{Provider: "claude", Access: access, Refresh: refresh, ExpiresAt: time.Now().Add(valid).UnixMilli()}
}

// openStores gives two stores over one credentials file, like the menu-bar
// app and a `patchbay login` run in a terminal.
func openStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	setHome(t, t.TempDir())
	a, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	return a, b
}

func stubRefresh(t *testing.T, fn func(Credentials) (Credentials, error)) {
	t.Helper()
	orig := refreshCredentials
	refreshCredentials = func(_ context.Context, _ config.Provider, cur Credentials) (Credentials, error) {
		return fn(cur)
	}
	t.Cleanup(func() { refreshCredentials = orig })
}

func TestStoreSeesOtherProcessWrites(t *testing.T) {
	app, cli := openStores(t)
	if err := app.Set("codex", Credentials{Access: "codex-1"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // distinct mtime
	if err := cli.Set("claude", creds("cli-login", "r-cli", time.Hour)); err != nil {
		t.Fatal(err)
	}
	if c, ok := app.Get("claude"); !ok || c.Access != "cli-login" {
		t.Fatalf("app did not see the terminal login: %+v %v", c, ok)
	}
	time.Sleep(10 * time.Millisecond)
	if err := app.Set("codex", Credentials{Access: "codex-2"}); err != nil {
		t.Fatal(err)
	}
	fresh, _ := OpenStore()
	if c, _ := fresh.Get("claude"); c.Access != "cli-login" {
		t.Fatalf("app's write erased the terminal login: %+v", c)
	}
}

func TestLoginDuringRefreshIsKept(t *testing.T) {
	store, _ := openStores(t)
	m := NewManager(store)
	if err := store.Set("claude", creds("old", "r-old", -time.Minute)); err != nil {
		t.Fatal(err)
	}
	stubRefresh(t, func(cur Credentials) (Credentials, error) {
		// The user finishes a fresh login while this refresh is in flight.
		if err := store.Set("claude", creds("login", "r-login", time.Hour)); err != nil {
			t.Error(err)
		}
		return creds("refreshed", "r-refreshed", time.Hour), nil
	})
	tok, err := m.AccessToken(context.Background(), claude)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "login" {
		t.Errorf("AccessToken = %q, want the new login's token", tok)
	}
	if c, _ := store.Get("claude"); c.Access != "login" || c.Refresh != "r-login" {
		t.Errorf("refresh overwrote the new login: %+v", c)
	}
}

func TestRefreshFailureUsesNewerLogin(t *testing.T) {
	store, _ := openStores(t)
	m := NewManager(store)
	if err := store.Set("claude", creds("old", "r-old", -time.Minute)); err != nil {
		t.Fatal(err)
	}
	stubRefresh(t, func(cur Credentials) (Credentials, error) {
		_ = store.Set("claude", creds("login", "r-login", time.Hour))
		return Credentials{}, errors.New("invalid_grant: refresh token revoked")
	})
	tok, err := m.AccessToken(context.Background(), claude)
	if err != nil || tok != "login" {
		t.Fatalf("AccessToken = %q, %v; want the new login's token", tok, err)
	}
}

func TestRefreshSavesWhenUncontended(t *testing.T) {
	store, _ := openStores(t)
	m := NewManager(store)
	if err := store.Set("claude", creds("old", "r-old", -time.Minute)); err != nil {
		t.Fatal(err)
	}
	stubRefresh(t, func(cur Credentials) (Credentials, error) {
		if cur.Refresh != "r-old" {
			t.Errorf("refreshed with %q, want the stored refresh token", cur.Refresh)
		}
		return creds("refreshed", "r-refreshed", time.Hour), nil
	})
	if tok, err := m.AccessToken(context.Background(), claude); err != nil || tok != "refreshed" {
		t.Fatalf("AccessToken = %q, %v", tok, err)
	}
	if c, _ := store.Get("claude"); c.Access != "refreshed" || c.Refresh != "r-refreshed" {
		t.Errorf("refresh not saved: %+v", c)
	}
}

func TestLogoutDuringRefreshSticks(t *testing.T) {
	store, _ := openStores(t)
	m := NewManager(store)
	if err := store.Set("claude", creds("old", "r-old", -time.Minute)); err != nil {
		t.Fatal(err)
	}
	stubRefresh(t, func(cur Credentials) (Credentials, error) {
		_ = store.Delete("claude")
		return creds("refreshed", "r-refreshed", time.Hour), nil
	})
	if _, err := m.AccessToken(context.Background(), claude); err == nil {
		t.Fatal("want an error after the user signed out mid-refresh")
	}
	if _, ok := store.Get("claude"); ok {
		t.Error("refresh re-created credentials the user deleted")
	}
}

// setHome points the user's home at dir for the test: HOME on macOS and
// Linux, USERPROFILE on Windows (where os.UserHomeDir ignores HOME).
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestStatusesProbeLocalServers(t *testing.T) {
	store, _ := openStores(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[]}`))
	}))
	defer up.Close()
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()

	cfg := &config.Config{Providers: []config.Provider{
		{ID: "ollama", Kind: config.KindOllama, BaseURL: up.URL, Enabled: true},
		{ID: "lmstudio", Kind: config.KindLMStudio, BaseURL: downURL, Enabled: true},
	}}
	st := NewManager(store).Statuses(cfg)
	if !st[0].Local || !st[0].SignedIn || st[0].NeedsKey || st[0].Address != up.Listener.Addr().String() {
		t.Errorf("running server: %+v", st[0])
	}
	if !st[1].Local || st[1].SignedIn || st[1].NeedsKey {
		t.Errorf("stopped server: %+v", st[1])
	}
}
