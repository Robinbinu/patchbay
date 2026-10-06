package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Anthropic (Claude Pro/Max) OAuth constants, from omp's anthropic.kdl rule.
// The client id is base64-encoded in the rule "so secret scanners stay quiet";
// it is a public OAuth client id, decoded here once.
var anthropicClientID = mustB64("OWQxYzI1MGEtZTYxYi00NGQ5LTg4ZWQtNTk0NGQxOTYyZjVl")

const (
	anthropicAuthorize   = "https://claude.ai/oauth/authorize"
	anthropicTokenURL    = "https://api.anthropic.com/v1/oauth/token"
	anthropicCallbackPt  = 54545
	anthropicCallback    = "/callback"
	anthropicBootstrapUR = "https://api.anthropic.com/api/claude_cli/bootstrap"
	anthropicOAuthBeta   = "oauth-2025-04-20"
	// Absolute lifetime of the grant family. omp observed ~30 days from the
	// interactive login regardless of refresh rotation; surfaced as a re-login
	// warning, not a wire contract.
	anthropicGrantTTL = 30 * 24 * time.Hour
)

var anthropicScopes = []string{
	"org:create_api_key",
	"user:profile",
	"user:inference",
	"user:sessions:claude_code",
	"user:mcp_servers",
	"user:file_upload",
}

// LoginAnthropic runs the Claude Pro/Max authorization-code (PKCE) flow.
func LoginAnthropic(ctx context.Context) (Credentials, error) {
	flow := loopbackFlow{
		port: anthropicCallbackPt,
		path: anthropicCallback,
		authorizeURL: func(state, redirectURI, challenge string) string {
			q := url.Values{
				"client_id":             {anthropicClientID},
				"response_type":         {"code"},
				"redirect_uri":          {redirectURI},
				"scope":                 {joinSpace(anthropicScopes)},
				"code_challenge":        {challenge},
				"code_challenge_method": {"S256"},
				"state":                 {state},
				"code":                  {"true"},
			}
			return anthropicAuthorize + "?" + q.Encode()
		},
	}
	code, verifier, redirectURI, state, err := flow.run(ctx, 5*time.Minute)
	if err != nil {
		return Credentials{}, err
	}
	c, err := anthropicExchange(ctx, map[string]any{
		"grant_type":    "authorization_code",
		"client_id":     anthropicClientID,
		"code":          code,
		"redirect_uri":  redirectURI,
		"code_verifier": verifier,
		"state":         state,
	}, "login")
	if err != nil {
		return Credentials{}, err
	}
	c.GrantExpiresAt = time.Now().Add(anthropicGrantTTL).UnixMilli()
	if c.AccountID == "" || c.Email == "" || c.OrgID == "" {
		anthropicBootstrap(ctx, &c, true)
	}
	return c, nil
}

// RefreshAnthropic exchanges the refresh token. Org identity is kept from the
// prior row (omp fixes it at login and never rewrites on refresh).
func RefreshAnthropic(ctx context.Context, prev Credentials) (Credentials, error) {
	if prev.Refresh == "" {
		return Credentials{}, errors.New("claude credentials have no refresh token; sign in again")
	}
	next, err := anthropicExchange(ctx, map[string]any{
		"grant_type":    "refresh_token",
		"client_id":     anthropicClientID,
		"refresh_token": prev.Refresh,
	}, "refresh")
	if err != nil {
		return Credentials{}, err
	}
	if next.Refresh == "" {
		next.Refresh = prev.Refresh
	}
	next.OrgID = prev.OrgID
	next.OrgName = prev.OrgName
	next.GrantExpiresAt = prev.GrantExpiresAt // refresh does not extend the grant family
	if next.AccountID == "" {
		next.AccountID = prev.AccountID
	}
	if next.Email == "" {
		next.Email = prev.Email
	}
	return next, nil
}

func anthropicExchange(ctx context.Context, body map[string]any, phase string) (Credentials, error) {
	reqCtx, cancel := context.WithTimeout(ctx, tokenHTTPTimeout)
	defer cancel()
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, anthropicTokenURL, bytes.NewReader(payload))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if phase == "refresh" {
		// Claude Code sends these on refresh; harmless and matches the server's
		// expectation for the rotating token.
		req.Header.Set("anthropic-beta", anthropicOAuthBeta)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	var tok struct {
		Access       string `json:"access_token"`
		Refresh      string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Account      struct {
			UUID         string `json:"uuid"`
			EmailAddress string `json:"email_address"`
		} `json:"account"`
		Organization struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
		} `json:"organization"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return Credentials{}, fmt.Errorf("anthropic token %s: decode: %w", phase, err)
	}
	if resp.StatusCode/100 != 2 || tok.Access == "" {
		return Credentials{}, fmt.Errorf("anthropic token %s failed: status %d", phase, resp.StatusCode)
	}
	// skew-ms=300000 in the rule: renew 5 minutes early.
	c := Credentials{
		Provider:  "claude",
		Access:    tok.Access,
		Refresh:   tok.Refresh,
		ExpiresAt: time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UnixMilli(),
		AccountID: tok.Account.UUID,
		Email:     tok.Account.EmailAddress,
		OrgID:     tok.Organization.UUID,
		OrgName:   tok.Organization.Name,
	}
	return c, nil
}

// anthropicBootstrap fills account/org identity from the claude_cli bootstrap
// endpoint when the token response omitted it (omp's anthropic-identity hook).
func anthropicBootstrap(ctx context.Context, c *Credentials, includeOrg bool) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	u := anthropicBootstrapUR + "?entrypoint=cli"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Access)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", anthropicOAuthBeta)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return
	}
	var data struct {
		OAuthAccount struct {
			AccountUUID      string `json:"account_uuid"`
			AccountEmail     string `json:"account_email"`
			OrganizationUUID string `json:"organization_uuid"`
			OrganizationName string `json:"organization_name"`
		} `json:"oauth_account"`
	}
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		return
	}
	if c.AccountID == "" {
		c.AccountID = data.OAuthAccount.AccountUUID
	}
	if c.Email == "" {
		c.Email = data.OAuthAccount.AccountEmail
	}
	if includeOrg {
		if c.OrgID == "" {
			c.OrgID = data.OAuthAccount.OrganizationUUID
		}
		if c.OrgName == "" {
			c.OrgName = data.OAuthAccount.OrganizationName
		}
	}
}

func mustB64(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func joinSpace(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out
}
