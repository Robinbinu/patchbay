// Package proxy serves the unified local endpoint: OpenAI-compatible
// /v1/chat/completions and /v1/responses, Anthropic-compatible /v1/messages,
// and an aggregated /v1/models. It authenticates local clients with the
// generated local API key and forwards each request to the provider that owns
// the requested model, injecting that provider's upstream credentials.
package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Robinbinu/patchbay/internal/auth"
	"github.com/Robinbinu/patchbay/internal/config"
	"github.com/Robinbinu/patchbay/internal/providers"
)

// Server is the HTTP proxy.
type Server struct {
	cfg *config.Config
	mgr *auth.Manager

	mu        sync.Mutex
	modelsIdx map[string]providers.Model // model id -> owning model record
	idxAt     time.Time
}

// New builds a proxy server.
func New(cfg *config.Config, mgr *auth.Manager) *Server {
	return &Server{cfg: cfg, mgr: mgr, modelsIdx: map[string]providers.Model{}}
}

// Handler returns the configured HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/models", s.auth(s.handleModels))
	mux.HandleFunc("/v1/chat/completions", s.auth(s.handleForward(providers.SurfaceChat)))
	mux.HandleFunc("/v1/responses", s.auth(s.handleForward(providers.SurfaceResponses)))
	mux.HandleFunc("/v1/messages", s.auth(s.handleForward(providers.SurfaceMessages)))
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "patchbay"})
}

// auth wraps a handler with local bearer-key enforcement.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "missing or invalid local API key")
			return
		}
		next(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	want := s.cfg.LocalKey()
	if want == "" {
		return true
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		if strings.TrimSpace(h[len("Bearer "):]) == want {
			return true
		}
	}
	if k := r.Header.Get("x-api-key"); k == want { // Anthropic-style clients
		return true
	}
	return false
}

// handleModels lists every routable model, sorted so tools' model pickers stay
// stable. Anthropic clients (they send anthropic-version) get the Anthropic
// list format and only the models that work on /v1/messages; everyone else
// gets the OpenAI format with each model's surface.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := s.refreshIndex(r.Context())
	sort.Slice(models, func(i, j int) bool { return canonical(models[i]) < canonical(models[j]) })

	if r.Header.Get("anthropic-version") != "" {
		data := make([]map[string]any, 0, len(models))
		for _, m := range models {
			if m.Surface != providers.SurfaceMessages {
				continue
			}
			name := m.Label
			if name == "" {
				name = m.ID
			}
			data = append(data, map[string]any{
				"type":         "model",
				"id":           canonical(m),
				"display_name": name,
				"created_at":   "1970-01-01T00:00:00Z", // upstream lists don't all carry dates
			})
		}
		resp := map[string]any{"data": data, "has_more": false, "first_id": nil, "last_id": nil}
		if len(data) > 0 {
			resp["first_id"], resp["last_id"] = data[0]["id"], data[len(data)-1]["id"]
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	data := make([]map[string]any, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]any{
			"id":       canonical(m), // provider-prefixed, e.g. "codex/gpt-5.6-luna"
			"object":   "model",
			"created":  0, // required by strict OpenAI clients; upstreams don't all provide it
			"owned_by": m.Provider,
			"surface":  m.Surface,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

// canonical is the provider-prefixed model id Patchbay advertises.
func canonical(m providers.Model) string { return m.Provider + "/" + m.ID }

// handleForward returns a handler for one API surface. It reads the body, finds
// the provider owning the requested model, and forwards.
func (s *Server) handleForward(surface string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "cannot read request body")
			return
		}
		model := extractModel(body)
		if model == "" {
			writeError(w, http.StatusBadRequest, "request body has no \"model\" field")
			return
		}
		p, bare, ok := s.providerForModel(r.Context(), model)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Sprintf(
				"unknown model %q: use an id from GET /v1/models, or prefix a provider id "+
					"(e.g. \"openai/%s\") to send a model that provider doesn't list", model, model))
			return
		}
		if bare != model {
			body = rewriteModel(body, bare)
		}
		s.forward(w, r, p, surface, body)
	}
}

// providerForModel resolves a requested model (provider-prefixed like
// "codex/gpt-5.6-luna", or a bare id) to its owning provider and the bare
// upstream model id to send on.
func (s *Server) providerForModel(ctx context.Context, model string) (config.Provider, string, bool) {
	lookup := func() (providers.Model, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		rec, ok := s.modelsIdx[model]
		return rec, ok
	}
	rec, ok := lookup()
	if !ok {
		s.refreshIndex(ctx)
		rec, ok = lookup()
	}
	if !ok {
		// An explicit "<provider>/<model>" reaches a model the provider
		// doesn't list (e.g. a local server without /models). A bare unknown
		// id is never guessed at: sending it to an arbitrary provider turns a
		// typo into a confusing upstream error.
		id, rest, found := strings.Cut(model, "/")
		if !found || rest == "" {
			return config.Provider{}, "", false
		}
		p, found := s.cfg.Provider(id)
		if !found || !p.Enabled {
			return config.Provider{}, "", false
		}
		return *p, rest, true
	}
	p, found := s.cfg.Provider(rec.Provider)
	if !found {
		return config.Provider{}, "", false
	}
	return *p, rec.ID, true
}

// refreshIndex rebuilds the model->provider index (cached 60s) and returns the
// flat model list.
func (s *Server) refreshIndex(ctx context.Context) []providers.Model {
	s.mu.Lock()
	fresh := time.Since(s.idxAt) < time.Minute && len(s.modelsIdx) > 0
	s.mu.Unlock()

	all := make([]providers.Model, 0, 64)
	if fresh {
		s.mu.Lock()
		for key, m := range s.modelsIdx {
			if key == canonical(m) { // skip bare-id aliases to avoid duplicates
				all = append(all, m)
			}
		}
		s.mu.Unlock()
		return all
	}

	idx := map[string]providers.Model{}
	for _, p := range s.cfg.Providers {
		if !p.Enabled {
			continue
		}
		ms, err := providers.Models(ctx, s.mgr, p)
		if err != nil {
			continue // a signed-out or failing provider should not blank the list
		}
		for _, m := range ms {
			// Canonical provider-prefixed id is the primary key; the bare id is
			// a convenience alias (first provider to claim it wins).
			idx[canonical(m)] = m
			if _, taken := idx[m.ID]; !taken {
				idx[m.ID] = m
			}
			all = append(all, m)
		}
	}
	if len(idx) > 0 {
		s.mu.Lock()
		s.modelsIdx = idx
		s.idxAt = time.Now()
		s.mu.Unlock()
	}
	return all
}

func extractModel(body []byte) string {
	var probe struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &probe)
	return probe.Model
}

// rewriteModel replaces the body's "model" with the bare upstream id, so a
// client may send a provider-prefixed id while the provider sees its own.
func rewriteModel(body []byte, model string) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	m["model"] = model
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "type": "patchbay_error"}})
}
