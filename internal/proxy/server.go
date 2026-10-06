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
			"id":       m.ID,
			"object":   "model",
			"owned_by": m.Provider,
			"surface":  m.Surface,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

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
		p, ok := s.providerForModel(r.Context(), model)
		if !ok {
			// Fall back to the first enabled provider whose surface matches.
			if fp, fok := s.providerForSurface(surface); fok {
				p = fp
			} else {
				writeError(w, http.StatusNotFound, fmt.Sprintf("no provider serves model %q", model))
				return
			}
		}
		s.forward(w, r, p, surface, body)
	}
}

func (s *Server) providerForModel(ctx context.Context, model string) (config.Provider, bool) {
	s.mu.Lock()
	rec, ok := s.modelsIdx[model]
	s.mu.Unlock()
	if !ok {
		s.refreshIndex(ctx)
		s.mu.Lock()
		rec, ok = s.modelsIdx[model]
		s.mu.Unlock()
	}
	if !ok {
		return config.Provider{}, false
	}
	p, found := s.cfg.Provider(rec.Provider)
	if !found {
		return config.Provider{}, false
	}
	return *p, true
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
		for _, m := range s.modelsIdx {
			all = append(all, m)
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
			idx[m.ID] = m
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "type": "patchbay_error"}})
}
