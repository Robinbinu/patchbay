package proxy

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/robin/patchbay/internal/config"
	"github.com/robin/patchbay/internal/providers"
)

// forward sends the (already-read) request body to the upstream that owns the
// chosen provider, injecting that provider's credentials and the per-surface
// headers, then streams the response straight back to the client.
func (s *Server) forward(w http.ResponseWriter, r *http.Request, p config.Provider, surface string, body []byte) {
	upstreamURL, err := upstreamURL(p, surface)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if accept := r.Header.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	}

	if err := s.applyUpstreamAuth(r, req, p); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// Copy content-type and stream the body (SSE or JSON) verbatim.
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 16<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			return
		}
		if readErr != nil {
			return
		}
	}
}

func upstreamURL(p config.Provider, surface string) (string, error) {
	base := providers.BaseURL(p)
	switch surface {
	case providers.SurfaceMessages:
		return base + "/v1/messages", nil
	case providers.SurfaceChat:
		return base + "/chat/completions", nil
	case providers.SurfaceResponses:
		if p.Kind == config.KindCodexOAuth {
			return base + "/codex/responses", nil
		}
		return base + "/responses", nil
	}
	return "", errBadSurface(surface)
}

type errBadSurface string

func (e errBadSurface) Error() string { return "unsupported API surface: " + string(e) }

// applyUpstreamAuth sets the provider's credentials and the fingerprint-neutral
// headers each surface requires.
func (s *Server) applyUpstreamAuth(r *http.Request, req *http.Request, p config.Provider) error {
	switch p.Kind {
	case config.KindCodexOAuth:
		token, err := s.mgr.AccessToken(r.Context(), p)
		if err != nil {
			return err
		}
		cred, _ := s.mgr.Store().Get(p.ID)
		req.Header.Set("Authorization", "Bearer "+token)
		if cred.AccountID != "" {
			req.Header.Set("chatgpt-account-id", cred.AccountID)
		}
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		req.Header.Set("originator", "patchbay")
		req.Header.Set("version", providers.CodexClientVersion())
		req.Header.Set("User-Agent", providers.CodexUserAgent())

	case config.KindAnthropicOAuth:
		token, err := s.mgr.AccessToken(r.Context(), p)
		if err != nil {
			return err
		}
		// Honest OAuth request: the bearer token and the oauth beta the token
		// grant requires. No Claude Code version/billing fingerprint.
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("anthropic-version", providers.AnthropicVersion())
		req.Header.Set("anthropic-beta", providers.AnthropicOAuthBeta())

	case config.KindAnthropicKey:
		req.Header.Set("x-api-key", p.APIKey)
		req.Header.Set("anthropic-version", providers.AnthropicVersion())
		if b := r.Header.Get("anthropic-beta"); b != "" {
			req.Header.Set("anthropic-beta", b)
		}

	case config.KindOpenAIKey:
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	return nil
}
