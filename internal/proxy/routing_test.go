package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robinbinu/patchbay/internal/config"
	"github.com/Robinbinu/patchbay/internal/providers"
)

// upstream records the model each forwarded request asked for.
func upstream(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Model string }
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		got = append(got, r.URL.Path+" "+body.Model)
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func chat(t *testing.T, s *Server, model string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"`+model+`","messages":[]}`))
	req.Header.Set("Authorization", "Bearer pby-test")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func routingServer(t *testing.T) (*Server, *[]string) {
	srv, got := upstream(t)
	s := testServer(providers.Model{ID: "gpt-5.5", Provider: "local", Surface: providers.SurfaceChat})
	s.cfg.Providers = []config.Provider{
		{ID: "local", Kind: config.KindOpenAIKey, BaseURL: srv.URL, APIKey: "sk-test", Enabled: true},
	}
	return s, got
}

func TestUnknownModelIsNotGuessed(t *testing.T) {
	s, got := routingServer(t)
	rec := chat(t, s, "claude-sonnet-4-20250514")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "/v1/models") {
		t.Errorf("error should point at /v1/models: %s", rec.Body)
	}
	if len(*got) != 0 {
		t.Errorf("unknown model was forwarded upstream: %v", *got)
	}
}

func TestRouting(t *testing.T) {
	cases := []struct{ model, want string }{
		{"gpt-5.5", "/chat/completions gpt-5.5"},               // listed, bare id
		{"local/gpt-5.5", "/chat/completions gpt-5.5"},         // listed, prefixed
		{"local/my-finetune", "/chat/completions my-finetune"}, // unlisted, explicit provider
	}
	for _, c := range cases {
		s, got := routingServer(t)
		if rec := chat(t, s, c.model); rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", c.model, rec.Code, rec.Body)
		}
		if len(*got) != 1 || (*got)[0] != c.want {
			t.Errorf("%s: upstream saw %v, want [%s]", c.model, *got, c.want)
		}
	}
	for _, model := range []string{"nope/gpt-5.5", "local/"} {
		s, got := routingServer(t)
		if rec := chat(t, s, model); rec.Code != http.StatusNotFound || len(*got) != 0 {
			t.Errorf("%s: status %d, upstream saw %v; want 404 and nothing forwarded", model, rec.Code, *got)
		}
	}
}
