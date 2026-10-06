package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// OpenRouter OAuth (PKCE), from omp's openrouter rule. There is no client id;
// the S256 verifier is the only proof of identity, OpenRouter never echoes
// state, and the exchange returns a durable API key (no refresh, no expiry).
const (
	openrouterAuthorize  = "https://openrouter.ai/auth"
	openrouterTokenURL   = "https://openrouter.ai/api/v1/auth/keys"
	openrouterCallbackPt = 54549
	openrouterCallback   = "/callback"
)

// LoginOpenRouter runs the OpenRouter PKCE flow and returns the durable key as
// the credential's access value.
func LoginOpenRouter(ctx context.Context) (Credentials, error) {
	flow := loopbackFlow{
		port: openrouterCallbackPt,
		path: openrouterCallback,
		authorizeURL: func(_state, redirectURI, challenge string) string {
			// Non-standard params: OpenRouter wants callback_url + code_challenge
			// and no client_id/response_type/scope.
			q := url.Values{
				"callback_url":          {redirectURI},
				"code_challenge":        {challenge},
				"code_challenge_method": {"S256"},
			}
			return openrouterAuthorize + "?" + q.Encode()
		},
	}
	code, verifier, _, _, err := flow.run(ctx, 5*time.Minute)
	if err != nil {
		return Credentials{}, err
	}
	payload, _ := json.Marshal(map[string]string{
		"code":                  code,
		"code_verifier":         verifier,
		"code_challenge_method": "S256",
	})
	reqCtx, cancel := context.WithTimeout(ctx, tokenHTTPTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, openrouterTokenURL, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Credentials{}, fmt.Errorf("openrouter key exchange decode: %w", err)
	}
	if resp.StatusCode/100 != 2 || out.Key == "" {
		return Credentials{}, fmt.Errorf("openrouter key exchange failed: status %d", resp.StatusCode)
	}
	// Durable key: stored as access with no expiry and no refresh.
	return Credentials{Provider: "openrouter", Access: out.Key}, nil
}

// RefreshOpenRouter never runs; the key is durable.
func RefreshOpenRouter(_ context.Context, prev Credentials) (Credentials, error) {
	if prev.Access == "" {
		return Credentials{}, errors.New("openrouter credentials missing; sign in again")
	}
	return prev, nil
}
