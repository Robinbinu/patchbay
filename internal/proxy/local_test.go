package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robinbinu/patchbay/internal/config"
	"github.com/Robinbinu/patchbay/internal/providers"
)

// A local server takes every surface at its standard path, so one model works
// from OpenAI and Anthropic clients alike.
func TestLocalServerTakesEverySurface(t *testing.T) {
	srv, got := upstream(t)
	s := testServer(providers.Model{ID: "llama3.2:latest", Provider: "ollama", Surface: providers.SurfaceAny})
	s.cfg.Providers = []config.Provider{{ID: "ollama", Kind: config.KindOllama, BaseURL: srv.URL + "/v1", Enabled: true}}

	for path, want := range map[string]string{
		"/v1/chat/completions": "/v1/chat/completions llama3.2:latest",
		"/v1/responses":        "/v1/responses llama3.2:latest",
		"/v1/messages":         "/v1/messages llama3.2:latest",
	} {
		*got = nil
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"ollama/llama3.2:latest"}`))
		req.Header.Set("Authorization", "Bearer pby-test")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || len(*got) != 1 || (*got)[0] != want {
			t.Errorf("%s: status %d, upstream saw %v; want [%s]", path, rec.Code, *got, want)
		}
	}
}

func TestLocalKeyAndVersionForwarded(t *testing.T) {
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	s := testServer(providers.Model{ID: "qwen3-8b", Provider: "lmstudio", Surface: providers.SurfaceAny})
	s.cfg.Providers = []config.Provider{{ID: "lmstudio", Kind: config.KindLMStudio, BaseURL: srv.URL, APIKey: "lm-secret", Enabled: true}}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"qwen3-8b"}`))
	req.Header.Set("x-api-key", "pby-test")
	req.Header.Set("anthropic-version", "2023-06-01")
	s.Handler().ServeHTTP(httptest.NewRecorder(), req)
	if hdr.Get("Authorization") != "Bearer lm-secret" || hdr.Get("x-api-key") != "lm-secret" {
		t.Errorf("local key not forwarded: Authorization=%q x-api-key=%q", hdr.Get("Authorization"), hdr.Get("x-api-key"))
	}
	if hdr.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("anthropic-version not passed through: %q", hdr.Get("anthropic-version"))
	}
}

func TestAnthropicClientsSeeLocalModels(t *testing.T) {
	s := testServer(append(catalog, providers.Model{ID: "llama3.2:latest", Provider: "ollama", Surface: providers.SurfaceAny})...)
	out := list(t, s, map[string]string{"x-api-key": "pby-test", "anthropic-version": "2023-06-01"})
	found := false
	for _, id := range ids(out["data"]) {
		if id == "ollama/llama3.2:latest" {
			found = true
		}
	}
	if !found {
		t.Errorf("Anthropic model list misses the local model: %v", ids(out["data"]))
	}
}
