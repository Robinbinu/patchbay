package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robinbinu/patchbay/internal/config"
	"github.com/Robinbinu/patchbay/internal/providers"
)

// testServer returns a server whose model index is already warm, so listing
// never reaches an upstream.
func testServer(models ...providers.Model) *Server {
	s := New(&config.Config{LocalAPIKey: "pby-test"}, nil)
	for _, m := range models {
		s.modelsIdx[canonical(m)] = m
		s.modelsIdx[m.ID] = m
	}
	s.idxAt = time.Now()
	return s
}

var catalog = []providers.Model{
	{ID: "gpt-6-luna", Provider: "codex", Surface: providers.SurfaceResponses, Label: "GPT-6 Luna"},
	{ID: "claude-sonnet-5-5", Provider: "claude", Surface: providers.SurfaceMessages, Label: "Claude Sonnet 5.5"},
	{ID: "openai/gpt-5.5", Provider: "openrouter", Surface: providers.SurfaceChat},
	{ID: "claude-opus-5-5", Provider: "claude", Surface: providers.SurfaceMessages},
}

func list(t *testing.T, s *Server, hdr map[string]string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func ids(data any) []string {
	var out []string
	for _, m := range data.([]any) {
		out = append(out, m.(map[string]any)["id"].(string))
	}
	return out
}

func TestModelsOpenAIFormat(t *testing.T) {
	s := testServer(catalog...)
	for i := 0; i < 5; i++ { // map order must not leak into the response
		out := list(t, s, map[string]string{"Authorization": "Bearer pby-test"})
		if out["object"] != "list" {
			t.Fatalf("object = %v", out["object"])
		}
		got := ids(out["data"])
		want := []string{"claude/claude-opus-5-5", "claude/claude-sonnet-5-5", "codex/gpt-6-luna", "openrouter/openai/gpt-5.5"}
		if len(got) != len(want) {
			t.Fatalf("ids = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("ids = %v, want %v", got, want)
			}
		}
		for _, m := range out["data"].([]any) {
			for _, k := range []string{"id", "object", "created", "owned_by", "surface"} {
				if _, ok := m.(map[string]any)[k]; !ok {
					t.Errorf("model %v missing %q", m, k)
				}
			}
		}
	}
}

func TestModelsAnthropicFormat(t *testing.T) {
	s := testServer(catalog...)
	out := list(t, s, map[string]string{"x-api-key": "pby-test", "anthropic-version": "2023-06-01"})
	got := ids(out["data"])
	if len(got) != 2 || got[0] != "claude/claude-opus-5-5" || got[1] != "claude/claude-sonnet-5-5" {
		t.Fatalf("Anthropic clients should see only messages models, sorted; got %v", got)
	}
	first := out["data"].([]any)[1].(map[string]any)
	if first["type"] != "model" || first["display_name"] != "Claude Sonnet 5.5" || first["created_at"] == nil {
		t.Errorf("unexpected model entry %v", first)
	}
	if out["has_more"] != false || out["first_id"] != "claude/claude-opus-5-5" || out["last_id"] != "claude/claude-sonnet-5-5" {
		t.Errorf("unexpected pagination fields %v", out)
	}
}

func TestModelsRequiresKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	testServer(catalog...).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}
