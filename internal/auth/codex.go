package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Codex (ChatGPT plan) OAuth constants, from omp's openai-codex rule. The
// client id is OpenAI's own public Codex CLI client; the redirect URI is the
// single value OpenAI allowlists, so the callback port is fixed.
const (
	codexClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexAuthorize   = "https://auth.openai.com/oauth/authorize"
	codexTokenURL    = "https://auth.openai.com/oauth/token"
	codexScope       = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	codexCallbackPt  = 1455
	codexCallback    = "/auth/callback"
	jwtAuthClaim     = "https://api.openai.com/auth"
	jwtProfileClaim  = "https://api.openai.com/profile"
	codexOriginator  = "patchbay"
	tokenHTTPTimeout = 15 * time.Second
)

// LoginCodex runs the Codex authorization-code (PKCE) flow and returns stored
// credentials. Matches omp: form-encoded token exchange, identity from the JWT.
func LoginCodex(ctx context.Context) (Credentials, error) {
	flow := loopbackFlow{
		port: codexCallbackPt,
		path: codexCallback,
		authorizeURL: func(state, redirectURI, challenge string) string {
			q := url.Values{
				"response_type":             {"code"},
				"client_id":                 {codexClientID},
				"redirect_uri":              {redirectURI},
				"scope":                     {codexScope},
				"code_challenge":            {challenge},
				"code_challenge_method":     {"S256"},
				"state":                     {state},
				"id_token_add_organizations": {"true"},
				"codex_cli_simplified_flow": {"true"},
				"originator":                {codexOriginator},
			}
			return codexAuthorize + "?" + q.Encode()
		},
	}
	code, verifier, redirectURI, _, err := flow.run(ctx, 5*time.Minute)
	if err != nil {
		return Credentials{}, err
	}
	return codexExchange(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {codexClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}, "login")
}

// RefreshCodex exchanges the refresh token for a fresh access token.
func RefreshCodex(ctx context.Context, prev Credentials) (Credentials, error) {
	if prev.Refresh == "" {
		return Credentials{}, errors.New("codex credentials have no refresh token; sign in again")
	}
	next, err := codexExchange(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {codexClientID},
		"refresh_token": {prev.Refresh},
	}, "refresh")
	if err != nil {
		return Credentials{}, err
	}
	if next.Refresh == "" {
		next.Refresh = prev.Refresh
	}
	if next.AccountID == "" {
		next.AccountID = prev.AccountID
	}
	if next.Email == "" {
		next.Email = prev.Email
	}
	if next.OrgID == "" {
		next.OrgID = prev.OrgID
	}
	if next.OrgName == "" {
		next.OrgName = prev.OrgName
	}
	return next, nil
}

func codexExchange(ctx context.Context, form url.Values, phase string) (Credentials, error) {
	reqCtx, cancel := context.WithTimeout(ctx, tokenHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	var tok struct {
		Access    string `json:"access_token"`
		Refresh   string `json:"refresh_token"`
		IDToken   string `json:"id_token"`
		ExpiresIn int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return Credentials{}, fmt.Errorf("codex token %s: decode: %w", phase, err)
	}
	if resp.StatusCode/100 != 2 || tok.Access == "" {
		return Credentials{}, fmt.Errorf("codex token %s failed: status %d", phase, resp.StatusCode)
	}
	acct, email, plan := codexProfile(tok.Access, tok.IDToken)
	if phase == "login" && acct == "" && email == "" {
		return Credentials{}, errors.New("codex token carried no account identity")
	}
	c := Credentials{
		Provider:  "codex",
		Access:    tok.Access,
		Refresh:   tok.Refresh,
		ExpiresAt: time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UnixMilli(),
		AccountID: acct,
		Email:     email,
		OrgID:     acct,
		OrgName:   plan,
	}
	return c, nil
}

// codexProfile reads account id, email and plan type from the Codex JWTs.
func codexProfile(access, idToken string) (accountID, email, plan string) {
	read := func(tok string) (string, string, string) {
		var claims struct {
			Auth struct {
				AccountID string `json:"chatgpt_account_id"`
				PlanType  string `json:"chatgpt_plan_type"`
			} `json:"https://api.openai.com/auth"`
			Profile struct {
				Email string `json:"email"`
			} `json:"https://api.openai.com/profile"`
		}
		if !decodeJWT(tok, &claims) {
			return "", "", ""
		}
		return claims.Auth.AccountID, strings.ToLower(strings.TrimSpace(claims.Profile.Email)), strings.ToLower(strings.TrimSpace(claims.Auth.PlanType))
	}
	accountID, email, plan = read(access)
	if idToken != "" {
		ia, ie, ip := read(idToken)
		if accountID == "" {
			accountID = ia
		}
		if email == "" {
			email = ie
		}
		if plan == "" {
			plan = ip
		}
	}
	return accountID, email, plan
}

func decodeJWT(token string, out any) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some tokens pad; fall back to std base64.
		payload, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return false
		}
	}
	return json.Unmarshal(payload, out) == nil
}
