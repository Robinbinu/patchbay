package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// xAI Grok OAuth (SuperGrok / X Premium+), from omp's xai-oauth rule. The token
// endpoint is discovered from the OIDC issuer; the device endpoint and client
// id are fixed. Adapted from NousResearch/hermes-agent (MIT), as omp notes.
const (
	xaiClientID    = "b1a00492-073a-47ea-816f-4c329264a828"
	xaiIssuer      = "https://auth.x.ai"
	xaiDeviceURL   = "https://auth.x.ai/oauth2/device/code"
	xaiUserinfoURL = "https://auth.x.ai/oauth2/userinfo"
	xaiScope       = "openid profile email offline_access grok-cli:access api:access"
)

// LoginXAI runs the device-code flow for Grok.
func LoginXAI(ctx context.Context) (Credentials, error) {
	tokenURL, err := xaiTokenEndpoint(ctx)
	if err != nil {
		return Credentials{}, err
	}
	flow := deviceFlow{
		clientID:  xaiClientID,
		scope:     xaiScope,
		deviceURL: xaiDeviceURL,
		tokenURL:  tokenURL,
		extraHdr:  map[string]string{"Accept": "application/json"},
	}
	tok, err := flow.run(ctx)
	if err != nil {
		return Credentials{}, err
	}
	c := xaiCredFromToken(tok)
	xaiIdentity(ctx, &c)
	return c, nil
}

// RefreshXAI exchanges the refresh token at the discovered endpoint.
func RefreshXAI(ctx context.Context, prev Credentials) (Credentials, error) {
	if prev.Refresh == "" {
		return Credentials{}, errors.New("grok credentials have no refresh token; sign in again")
	}
	tokenURL, err := xaiTokenEndpoint(ctx)
	if err != nil {
		return Credentials{}, err
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {xaiClientID},
		"refresh_token": {prev.Refresh},
	}
	reqCtx, cancel := context.WithTimeout(ctx, tokenHTTPTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return Credentials{}, fmt.Errorf("grok token refresh decode: %w", err)
	}
	if resp.StatusCode/100 != 2 || tok.AccessToken == "" {
		return Credentials{}, fmt.Errorf("grok token refresh failed: status %d", resp.StatusCode)
	}
	next := xaiCredFromToken(tok)
	if next.Refresh == "" {
		next.Refresh = prev.Refresh
	}
	if next.AccountID == "" {
		next.AccountID = prev.AccountID
	}
	if next.Email == "" {
		next.Email = prev.Email
	}
	return next, nil
}

func xaiCredFromToken(tok tokenResponse) Credentials {
	c := Credentials{
		Provider: "grok",
		Access:   tok.AccessToken,
		Refresh:  tok.RefreshToken,
	}
	if tok.ExpiresIn > 0 {
		c.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UnixMilli()
	}
	// Identity from the access-token JWT when present.
	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if decodeJWT(tok.AccessToken, &claims) {
		c.AccountID = claims.Sub
		c.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	}
	return c
}

// xaiTokenEndpoint resolves token_endpoint from the OIDC discovery document,
// requiring an https x.ai host (matching omp's validateXAIEndpoint).
func xaiTokenEndpoint(ctx context.Context) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, tokenHTTPTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, xaiIssuer+"/.well-known/openid-configuration", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var disc struct {
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil {
		return "", fmt.Errorf("xai discovery decode: %w", err)
	}
	if disc.TokenEndpoint == "" {
		return "", errors.New("xai discovery: no token_endpoint")
	}
	u, err := url.Parse(disc.TokenEndpoint)
	if err != nil || u.Scheme != "https" || !(u.Hostname() == "x.ai" || strings.HasSuffix(u.Hostname(), ".x.ai")) {
		return "", fmt.Errorf("xai discovery: invalid token_endpoint %q", disc.TokenEndpoint)
	}
	return disc.TokenEndpoint, nil
}

func xaiIdentity(ctx context.Context, c *Credentials) {
	if c.Email != "" && c.AccountID != "" {
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, tokenHTTPTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, xaiUserinfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+c.Access)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return
	}
	var info struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if json.NewDecoder(resp.Body).Decode(&info) != nil {
		return
	}
	if c.AccountID == "" {
		c.AccountID = info.Sub
	}
	if c.Email == "" {
		c.Email = strings.ToLower(strings.TrimSpace(info.Email))
	}
}
