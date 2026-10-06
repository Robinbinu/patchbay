package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Robinbinu/patchbay/internal/config"
)

// Per-provider access-token renewal skew, from the omp rules (Codex skew 0,
// Anthropic skew 5m).
func skewFor(kind string) time.Duration {
	switch kind {
	case config.KindAnthropicOAuth:
		return 5 * time.Minute
	default:
		return 0
	}
}

// Manager coordinates logins and background-safe refreshes over the store.
// A per-provider mutex collapses concurrent refreshes into one.
type Manager struct {
	store *Store
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewManager wraps a credential store.
func NewManager(store *Store) *Manager {
	return &Manager{store: store, locks: map[string]*sync.Mutex{}}
}

// Store exposes the underlying credential store (read-only uses).
func (m *Manager) Store() *Store { return m.store }

func (m *Manager) lockFor(id string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks[id] == nil {
		m.locks[id] = &sync.Mutex{}
	}
	return m.locks[id]
}

// Login runs the interactive OAuth flow for a provider kind and persists the
// result under the provider id.
func (m *Manager) Login(ctx context.Context, p config.Provider) (Credentials, error) {
	var (
		c   Credentials
		err error
	)
	switch p.Kind {
	case config.KindCodexOAuth:
		c, err = LoginCodex(ctx)
	case config.KindAnthropicOAuth:
		c, err = LoginAnthropic(ctx)
	case config.KindXAIOAuth:
		c, err = LoginXAI(ctx)
	case config.KindOpenRouterOAuth:
		c, err = LoginOpenRouter(ctx)
	default:
		return Credentials{}, fmt.Errorf("provider %q (kind %s) is not an OAuth provider", p.ID, p.Kind)
	}
	if err != nil {
		return Credentials{}, err
	}
	c.Provider = p.ID
	if err := m.store.Set(p.ID, c); err != nil {
		return Credentials{}, err
	}
	return c, nil
}

// AccessToken returns a currently-valid access token for an OAuth provider,
// refreshing in the background if the stored token is within its skew window.
func (m *Manager) AccessToken(ctx context.Context, p config.Provider) (string, error) {
	cur, ok := m.store.Get(p.ID)
	if !ok {
		return "", fmt.Errorf("not signed in to %q; run `patchbay login %s`", p.ID, p.ID)
	}
	if !cur.Expired(skewFor(p.Kind)) {
		return cur.Access, nil
	}
	if cur.GrantExpired() {
		return "", fmt.Errorf("the %q login has fully expired; run `patchbay login %s` again", p.ID, p.ID)
	}

	lock := m.lockFor(p.ID)
	lock.Lock()
	defer lock.Unlock()
	// Re-read: another goroutine may have refreshed, or a login replaced the
	// grant, while we waited.
	skew := skewFor(p.Kind)
	if cur, ok = m.store.Get(p.ID); !ok {
		return "", fmt.Errorf("not signed in to %q; run `patchbay login %s`", p.ID, p.ID)
	}
	if !cur.Expired(skew) {
		return cur.Access, nil
	}

	next, err := refreshCredentials(ctx, p, cur)
	if err != nil {
		// A login may have replaced the grant mid-refresh (and revoked the
		// refresh token we used); the new one is what we want anyway.
		if latest, ok := m.store.Get(p.ID); ok && latest.Refresh != cur.Refresh && !latest.Expired(skew) {
			return latest.Access, nil
		}
		return "", err
	}
	next.Provider = p.ID
	stored, saved, err := m.store.CompareAndSet(p.ID, cur, next)
	if err != nil {
		return "", err
	}
	if !saved {
		// Someone logged in or out while we refreshed; keep their change.
		switch {
		case stored.Access == "":
			return "", fmt.Errorf("signed out of %q", p.ID)
		case !stored.Expired(skew):
			return stored.Access, nil
		}
	}
	return next.Access, nil
}

// refreshCredentials exchanges a provider's refresh token; tests replace it.
var refreshCredentials = func(ctx context.Context, p config.Provider, cur Credentials) (Credentials, error) {
	switch p.Kind {
	case config.KindCodexOAuth:
		return RefreshCodex(ctx, cur)
	case config.KindAnthropicOAuth:
		return RefreshAnthropic(ctx, cur)
	case config.KindXAIOAuth:
		return RefreshXAI(ctx, cur)
	case config.KindOpenRouterOAuth:
		return RefreshOpenRouter(ctx, cur)
	}
	return Credentials{}, fmt.Errorf("provider %q is not an OAuth provider", p.ID)
}

// Status is a display snapshot of one provider's sign-in state.
type Status struct {
	ID           string
	Label        string
	Kind         string
	SignedIn     bool
	Email        string
	Plan         string // OrgName (plan type for Codex, org name for Claude)
	AccessExpiry time.Time
	GrantExpiry  time.Time
	Expired      bool // access token past deadline
	GrantExpired bool // whole grant gone; needs interactive re-login
	NeedsKey     bool // key-based provider missing its key
}

// Statuses builds a status row for every configured provider.
func (m *Manager) Statuses(cfg *config.Config) []Status {
	out := make([]Status, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		s := Status{ID: p.ID, Label: p.Label, Kind: p.Kind}
		switch {
		case config.OAuthKind(p.Kind):
			if c, ok := m.store.Get(p.ID); ok {
				s.SignedIn = true
				s.Email = c.Email
				s.Plan = c.OrgName
				if c.ExpiresAt > 0 {
					s.AccessExpiry = time.UnixMilli(c.ExpiresAt)
				}
				if c.GrantExpiresAt > 0 {
					s.GrantExpiry = time.UnixMilli(c.GrantExpiresAt)
				}
				s.Expired = c.Expired(0)
				s.GrantExpired = c.GrantExpired()
			}
		default:
			s.SignedIn = p.APIKey != ""
			s.NeedsKey = p.APIKey == ""
		}
		out = append(out, s)
	}
	return out
}
