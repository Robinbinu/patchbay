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
	"strings"
	"sync"
	"time"

	"github.com/robin/patchbay/internal/auth"
	"github.com/robin/patchbay/internal/config"
	"github.com/robin/patchbay/internal/providers"
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
	want := s.cfg.LocalAPIKey
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

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := s.refreshIndex(r.Context())
	data := make([]map[string]any, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]any{
			"id":       canonical(m), // provider-prefixed, e.g. "codex/gpt-5.6-luna"
			"object":   "model",
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
			// Fall back to the first enabled provider whose surface matches;
			// forward the model id unchanged.
			if fp, fok := s.providerForSurface(surface); fok {
				p, bare = fp, model
			} else {
				writeError(w, http.StatusNotFound, fmt.Sprintf("no provider serves model %q", model))
				return
			}
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
		return config.Provider{}, "", false
	}
	p, found := s.cfg.Provider(rec.Provider)
	if !found {
		return config.Provider{}, "", false
	}
	return *p, rec.ID, true
}

func (s *Server) providerForSurface(surface string) (config.Provider, bool) {
	for _, p := range s.cfg.Providers {
		if p.Enabled && providers.Surface(p.Kind) == surface {
			return p, true
		}
	}
	return config.Provider{}, false
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
