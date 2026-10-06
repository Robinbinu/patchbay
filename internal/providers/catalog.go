// Package providers knows each upstream's base URL, auth shape, and model list,
// and resolves which configured provider serves a given model id.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/robin/patchbay/internal/auth"
	"github.com/robin/patchbay/internal/config"
)

// API surfaces Patchbay exposes and routes to.
const (
	SurfaceMessages  = "messages" // Anthropic /v1/messages
	SurfaceChat      = "chat"     // OpenAI /v1/chat/completions
	SurfaceResponses = "responses"
)

// Codex backend constants (omp wire/codex.ts). Honest Patchbay identity on the
// originator/user-agent; the version is the gate the backend checks for model
// availability, so it is sent as-is.
const (
	codexBase          = "https://chatgpt.com/backend-api"
	codexClientVersion = "0.159.0"
	codexUserAgent     = "patchbay/0.1 (+https://github.com/robin/patchbay)"
	anthropicBase      = "https://api.anthropic.com"
	openaiBase         = "https://api.openai.com/v1"
	xaiBase            = "https://api.x.ai/v1"
	openrouterBase     = "https://openrouter.ai/api/v1"
	anthropicVersion   = "2023-06-01"
	anthropicOAuthBeta = "oauth-2025-04-20"
)

// Model is one routable model advertised by a provider.
type Model struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Surface  string `json:"surface"`
	Label    string `json:"label,omitempty"`
}

// BaseURL returns the effective upstream base for a provider.
func BaseURL(p config.Provider) string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	switch p.Kind {
	case config.KindCodexOAuth:
		return codexBase
	case config.KindAnthropicOAuth, config.KindAnthropicKey:
		return anthropicBase
	case config.KindOpenAIKey:
		return openaiBase
	case config.KindXAIOAuth:
		return xaiBase
	case config.KindOpenRouterOAuth:
		return openrouterBase
	}
	return ""
}

// Surface returns the API surface a provider kind speaks.
func Surface(kind string) string {
	switch kind {
	case config.KindAnthropicOAuth, config.KindAnthropicKey:
		return SurfaceMessages
	case config.KindCodexOAuth, config.KindXAIOAuth:
		return SurfaceResponses
	case config.KindOpenAIKey, config.KindOpenRouterOAuth:
		return SurfaceChat
	}
	return ""
}

// Models fetches a provider's model list from its upstream.
func Models(ctx context.Context, mgr *auth.Manager, p config.Provider) ([]Model, error) {
	switch p.Kind {
	case config.KindCodexOAuth:
		return codexModels(ctx, mgr, p)
	case config.KindAnthropicOAuth, config.KindAnthropicKey:
		return anthropicModels(ctx, mgr, p)
	case config.KindOpenAIKey:
		return openAIModels(ctx, p)
	case config.KindXAIOAuth:
		return bearerModels(ctx, mgr, p, SurfaceResponses)
	case config.KindOpenRouterOAuth:
		return bearerModels(ctx, mgr, p, SurfaceChat)
	}
	return nil, fmt.Errorf("unknown provider kind %q", p.Kind)
}

func codexModels(ctx context.Context, mgr *auth.Manager, p config.Provider) ([]Model, error) {
	token, err := mgr.AccessToken(ctx, p)
	if err != nil {
		return nil, err
	}
	cred, _ := mgr.Store().Get(p.ID)
	u := BaseURL(p) + "/codex/models?client_version=" + codexClientVersion
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if cred.AccountID != "" {
		req.Header.Set("chatgpt-account-id", cred.AccountID)
	}
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("originator", "patchbay")
	req.Header.Set("version", codexClientVersion)
	req.Header.Set("Accept", "application/json")
	body, err := doJSON(req)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Models []struct {
			Slug        string `json:"slug"`
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"models"`
		Data []struct {
			Slug        string `json:"slug"`
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	entries := parsed.Models
	if len(entries) == 0 {
		entries = parsed.Data
	}
	out := make([]Model, 0, len(entries))
	for _, e := range entries {
		id := e.Slug
		if id == "" {
			id = e.ID
		}
		if id == "" {
			continue
		}
		out = append(out, Model{ID: id, Provider: p.ID, Surface: SurfaceResponses, Label: e.DisplayName})
	}
	return out, nil
}

func anthropicModels(ctx context.Context, mgr *auth.Manager, p config.Provider) ([]Model, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL(p)+"/v1/models", nil)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("Accept", "application/json")
	if p.Kind == config.KindAnthropicOAuth {
		token, err := mgr.AccessToken(ctx, p)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("anthropic-beta", anthropicOAuthBeta)
	} else {
		req.Header.Set("x-api-key", p.APIKey)
	}
	body, err := doJSON(req)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(parsed.Data))
	for _, e := range parsed.Data {
		out = append(out, Model{ID: e.ID, Provider: p.ID, Surface: SurfaceMessages, Label: e.DisplayName})
	}
	return out, nil
}

// bearerModels lists an OpenAI-style /models for an OAuth provider whose token
// goes in Authorization: Bearer (xAI, OpenRouter).
func bearerModels(ctx context.Context, mgr *auth.Manager, p config.Provider, surface string) ([]Model, error) {
	token, err := mgr.AccessToken(ctx, p)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL(p)+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	body, err := doJSON(req)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(parsed.Data))
	for _, e := range parsed.Data {
		out = append(out, Model{ID: e.ID, Provider: p.ID, Surface: surface})
	}
	return out, nil
}

func openAIModels(ctx context.Context, p config.Provider) ([]Model, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL(p)+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Accept", "application/json")
	body, err := doJSON(req)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(parsed.Data))
	for _, e := range parsed.Data {
		out = append(out, Model{ID: e.ID, Provider: p.ID, Surface: SurfaceChat})
	}
	return out, nil
}

func doJSON(req *http.Request) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf := new(strings.Builder)
	// Cap model-list reads defensively.
	const max = 8 << 20
	b := make([]byte, 0, 64<<10)
	tmp := make([]byte, 32<<10)
	for {
		n, e := resp.Body.Read(tmp)
		if n > 0 {
			b = append(b, tmp[:n]...)
			if len(b) > max {
				break
			}
		}
		if e != nil {
			break
		}
	}
	_ = buf
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("upstream %s: status %d: %s", req.URL.Host, resp.StatusCode, truncate(string(b), 300))
	}
	return b, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
