package proxy

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestRotateWhileServing rotates the local key while requests authenticate.
// Run with -race: the key is read per request and written by the menu.
func TestRotateWhileServing(t *testing.T) {
	s := testServer(catalog...)
	h := s.Handler()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
				req.Header.Set("Authorization", "Bearer "+s.cfg.LocalKey())
				h.ServeHTTP(httptest.NewRecorder(), req)
			}
		}()
	}
	for j := 0; j < 200; j++ {
		s.cfg.RotateLocalKey()
	}
	wg.Wait()

	key := s.cfg.LocalKey()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("current key rejected: %d", rec.Code)
	}
}
