package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robinbinu/patchbay/internal/config"
)

func TestLocalRoot(t *testing.T) {
	cases := []struct {
		p    config.Provider
		want string
	}{
		{config.Provider{Kind: config.KindOllama}, "http://127.0.0.1:11434"},
		{config.Provider{Kind: config.KindLMStudio}, "http://127.0.0.1:1234"},
		{config.Provider{Kind: config.KindOllama, BaseURL: "http://gpu-box:11434/v1/"}, "http://gpu-box:11434"},
		{config.Provider{Kind: config.KindLMStudio, BaseURL: "http://localhost:4321"}, "http://localhost:4321"},
	}
	for _, c := range cases {
		if got := LocalRoot(c.p); got != c.want {
			t.Errorf("LocalRoot(%+v) = %q, want %q", c.p, got, c.want)
		}
	}
	for _, k := range []string{config.KindOllama, config.KindLMStudio} {
		if Surface(k) != SurfaceAny {
			t.Errorf("Surface(%s) = %q, want any", k, Surface(k))
		}
	}
}

func TestLocalModels(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		w.Write([]byte(`{"object":"list","data":[{"id":"llama3.2:latest"},{"id":"qwen3:8b"}]}`))
	}))
	defer srv.Close()

	p := config.Provider{ID: "ollama", Kind: config.KindOllama, BaseURL: srv.URL}
	ms, err := Models(context.Background(), nil, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].ID != "llama3.2:latest" || ms[0].Provider != "ollama" || ms[0].Surface != SurfaceAny {
		t.Fatalf("models = %+v", ms)
	}
	if auth != "" {
		t.Errorf("sent Authorization %q with no key configured", auth)
	}
	p.APIKey = "lm-secret"
	if _, err := Models(context.Background(), nil, p); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer lm-secret" {
		t.Errorf("Authorization = %q, want the configured key", auth)
	}
}

func TestLocalModelsServerDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listening any more
	if _, err := Models(context.Background(), nil, config.Provider{ID: "ollama", Kind: config.KindOllama, BaseURL: url}); err == nil {
		t.Fatal("want an error from a stopped server")
	}
}
