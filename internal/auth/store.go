package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Robinbinu/patchbay/internal/config"
)

// Credentials is one provider's OAuth grant, stored in ~/.patchbay/credentials.json.
// ExpiresAt is an absolute unix-milliseconds deadline for the access token.
// GrantExpiresAt, when non-zero, is the absolute lifetime of the whole grant
// family (Anthropic's ~30-day re-login deadline); refresh does not extend it.
type Credentials struct {
	Provider       string `json:"provider"`
	Access         string `json:"access"`
	Refresh        string `json:"refresh"`
	ExpiresAt      int64  `json:"expires_at"`                 // unix ms; access-token deadline
	GrantExpiresAt int64  `json:"grant_expires_at,omitempty"` // unix ms; whole-grant deadline
	AccountID      string `json:"account_id,omitempty"`
	Email          string `json:"email,omitempty"`
	OrgID          string `json:"org_id,omitempty"`
	OrgName        string `json:"org_name,omitempty"` // plan type for Codex
}

// Expired reports whether the access token is past its deadline minus skew.
func (c Credentials) Expired(skew time.Duration) bool {
	if c.ExpiresAt == 0 {
		return false
	}
	deadline := time.UnixMilli(c.ExpiresAt).Add(-skew)
	return time.Now().After(deadline)
}

// GrantExpired reports whether the whole grant family has aged out and only a
// fresh interactive login will recover it.
func (c Credentials) GrantExpired() bool {
	if c.GrantExpiresAt == 0 {
		return false
	}
	return time.Now().After(time.UnixMilli(c.GrantExpiresAt))
}

// Store is the on-disk credential set, one entry per provider id. The file is
// shared with other Patchbay processes (`patchbay login` in a terminal while
// the menu-bar app serves), so every operation first picks up changes made on
// disk, and writes merge into the latest contents instead of replacing them.
type Store struct {
	mu    sync.Mutex
	path  string
	items map[string]Credentials
	seen  os.FileInfo // the file as last read or written; nil if absent
}

// OpenStore loads ~/.patchbay/credentials.json (empty if absent).
func OpenStore() (*Store, error) {
	d, err := config.Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(d, "credentials.json"), items: map[string]Credentials{}}
	if err := s.reloadLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// reloadLocked re-reads the file if it changed since this store last saw it.
func (s *Store) reloadLocked() error {
	fi, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if s.seen != nil { // removed by another process
			s.items, s.seen = map[string]Credentials{}, nil
		}
		return nil
	}
	if err != nil {
		return err
	}
	if s.seen != nil && fi.ModTime().Equal(s.seen.ModTime()) && fi.Size() == s.seen.Size() {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	items := map[string]Credentials{}
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	s.items, s.seen = items, fi
	return nil
}

// Get returns the credentials for a provider id. If the file can't be re-read
// (say, mid-write by another process) the last good copy is used.
func (s *Store) Get(id string) (Credentials, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadLocked()
	c, ok := s.items[id]
	return c, ok
}

// Set stores (and persists) credentials for a provider id.
func (s *Store) Set(id string, c Credentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return err
	}
	s.items[id] = c
	return s.saveLocked()
}

// CompareAndSet stores next only if the provider's credentials are still old
// (same access and refresh token). Otherwise, say a login replaced them while
// old was being refreshed, it leaves them alone. It returns what is stored
// afterwards (zero if nothing) and whether next was written.
func (s *Store) CompareAndSet(id string, old, next Credentials) (Credentials, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return Credentials{}, false, err
	}
	cur, ok := s.items[id]
	if !ok || cur.Access != old.Access || cur.Refresh != old.Refresh {
		return cur, false, nil
	}
	s.items[id] = next
	return next, true, s.saveLocked()
}

// Delete removes a provider's credentials.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return err
	}
	delete(s.items, id)
	return s.saveLocked()
}

// IDs lists the provider ids with stored credentials.
func (s *Store) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadLocked()
	ids := make([]string, 0, len(s.items))
	for id := range s.items {
		ids = append(ids, id)
	}
	return ids
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		return err
	}
	s.seen = fi
	return nil
}
