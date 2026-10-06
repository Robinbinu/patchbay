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

// deviceAuth is the RFC 8628 device-authorization response.
type deviceAuth struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpiresIn               int    `json:"expires_in"`
}

// deviceFlow runs an RFC 8628 device-code grant: request a user code, show the
// verification URL, then poll the token endpoint until the user approves.
type deviceFlow struct {
	clientID   string
	scope      string
	deviceURL  string
	tokenURL   string            // resolved endpoint (xAI discovers it via OIDC)
	extraHdr   map[string]string // provider fingerprint/accept headers
	grantType  string            // defaults to the RFC device_code grant
}

func (f deviceFlow) run(ctx context.Context) (tokenResponse, error) {
	da, err := f.authorize(ctx)
	if err != nil {
		return tokenResponse{}, err
	}
	shown := da.VerificationURIComplete
	if shown == "" {
		shown = da.VerificationURI
	}
	fmt.Printf("\nTo sign in, open:\n\n  %s\n\nand enter code:  %s\n\nWaiting for approval…\n", shown, da.UserCode)
	_ = OpenBrowser(shown)

	interval := time.Duration(da.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(maxInt(da.ExpiresIn, 300)) * time.Second)
	grant := f.grantType
	if grant == "" {
		grant = "urn:ietf:params:oauth:grant-type:device_code"
	}
	for {
		if time.Now().After(deadline) {
			return tokenResponse{}, errors.New("device authorization expired before approval")
		}
		select {
		case <-ctx.Done():
			return tokenResponse{}, ctx.Err()
		case <-time.After(interval):
		}
		tok, retry, err := f.poll(ctx, grant, da.DeviceCode)
		if err != nil {
			return tokenResponse{}, err
		}
		if retry == "slow_down" {
			interval += 5 * time.Second
			continue
		}
		if retry == "authorization_pending" {
			continue
		}
		return tok, nil
	}
}

func (f deviceFlow) authorize(ctx context.Context) (deviceAuth, error) {
	form := url.Values{"client_id": {f.clientID}}
	if f.scope != "" {
		form.Set("scope", f.scope)
	}
	reqCtx, cancel := context.WithTimeout(ctx, tokenHTTPTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, f.deviceURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for k, v := range f.extraHdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return deviceAuth{}, err
	}
	defer resp.Body.Close()
	var da deviceAuth
	if err := json.NewDecoder(resp.Body).Decode(&da); err != nil {
		return deviceAuth{}, fmt.Errorf("device authorization decode: %w", err)
	}
	if resp.StatusCode/100 != 2 || da.DeviceCode == "" {
		return deviceAuth{}, fmt.Errorf("device authorization failed: status %d", resp.StatusCode)
	}
	return da, nil
}

// poll returns (token, retryReason, err). retryReason is "authorization_pending"
// or "slow_down" when the caller should wait and poll again.
func (f deviceFlow) poll(ctx context.Context, grant, deviceCode string) (tokenResponse, string, error) {
	form := url.Values{
		"client_id":   {f.clientID},
		"device_code": {deviceCode},
		"grant_type":  {grant},
	}
	reqCtx, cancel := context.WithTimeout(ctx, tokenHTTPTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, f.tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for k, v := range f.extraHdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return tokenResponse{}, "", err
	}
	defer resp.Body.Close()
	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return tokenResponse{}, "", fmt.Errorf("device token decode: %w", err)
	}
	if resp.StatusCode/100 == 2 && tok.AccessToken != "" {
		return tok, "", nil
	}
	switch tok.Error {
	case "authorization_pending":
		return tokenResponse{}, "authorization_pending", nil
	case "slow_down":
		return tokenResponse{}, "slow_down", nil
	case "":
		return tokenResponse{}, "", fmt.Errorf("device token failed: status %d", resp.StatusCode)
	default:
		return tokenResponse{}, "", fmt.Errorf("device token failed: %s", tok.Error)
	}
}

// tokenResponse is the shared OAuth token-endpoint shape.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
