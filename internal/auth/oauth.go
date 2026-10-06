package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// callbackResult is the authorization code (and echoed state) returned to the
// local redirect listener.
type callbackResult struct {
	code  string
	state string
	err   error
}

// loopbackFlow runs an authorization-code grant against a fixed loopback
// redirect URI, the shape both Codex (port 1455) and Anthropic (port 54545)
// register. The port is not negotiable: both providers allowlist the exact URI.
type loopbackFlow struct {
	port         int
	path         string
	authorizeURL func(state, redirectURI, challenge string) string
}

func (f loopbackFlow) redirectURI() string {
	return fmt.Sprintf("http://localhost:%d%s", f.port, f.path)
}

// run opens the browser to the authorization URL and blocks until the provider
// redirects back with a code, the context is cancelled, or the timeout fires.
// It returns the code, the PKCE verifier used, and the redirect URI.
func (f loopbackFlow) run(ctx context.Context, timeout time.Duration) (code, verifier, redirectURI, state string, err error) {
	pkce, err := NewPKCE()
	if err != nil {
		return "", "", "", "", err
	}
	state, err = randomState()
	if err != nil {
		return "", "", "", "", err
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", f.port))
	if err != nil {
		return "", "", "", "", fmt.Errorf("port %d is busy; close whatever is using it and retry: %w", f.port, err)
	}

	results := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(f.path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			desc := q.Get("error_description")
			if desc == "" {
				desc = e
			}
			writeBrowserPage(w, false, desc)
			results <- callbackResult{err: fmt.Errorf("authorization failed: %s", desc)}
			return
		}
		got := q.Get("code")
		if got == "" {
			writeBrowserPage(w, false, "no authorization code in callback")
			results <- callbackResult{err: errors.New("callback missing authorization code")}
			return
		}
		if st := q.Get("state"); st != "" && state != "" && st != state {
			writeBrowserPage(w, false, "state mismatch")
			results <- callbackResult{err: errors.New("state mismatch (possible CSRF)")}
			return
		}
		writeBrowserPage(w, true, "")
		results <- callbackResult{code: got, state: q.Get("state")}
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	authURL := f.authorizeURL(state, f.redirectURI(), pkce.Challenge)
	_ = openBrowser(authURL)
	fmt.Printf("\nOpen this URL to sign in if your browser did not:\n\n  %s\n\n", authURL)

	select {
	case <-ctx.Done():
		return "", "", "", "", ctx.Err()
	case <-time.After(timeout):
		return "", "", "", "", errors.New("timed out waiting for the browser callback")
	case res := <-results:
		if res.err != nil {
			return "", "", "", "", res.err
		}
		// Some providers echo code#state; the fragment half wins.
		c := res.code
		if i := strings.IndexByte(c, '#'); i >= 0 {
			c = c[:i]
		}
		return c, pkce.Verifier, f.redirectURI(), state, nil
	}
}

func writeBrowserPage(w http.ResponseWriter, ok bool, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := "Patchbay — signed in"
	body := "You are signed in. You can close this tab and return to Patchbay."
	if !ok {
		title = "Patchbay — sign-in failed"
		body = "Sign-in failed: " + detail
	}
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%s</title>
<body style="font:16px -apple-system,system-ui,sans-serif;display:grid;place-items:center;height:100vh;margin:0;background:#0b0b0f;color:#e7e7ea">
<div style="max-width:28rem;padding:2rem;text-align:center">
<div style="font-size:2rem;margin-bottom:.5rem">%s</div>
<h1 style="font-size:1.1rem;margin:0 0 .5rem">%s</h1>
<p style="opacity:.7;margin:0">%s</p></div>`,
		title, map[bool]string{true: "✓", false: "⚠"}[ok], title, body)
}

// openBrowser best-effort launches the OS browser.
func openBrowser(target string) error {
	if _, err := url.Parse(target); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}
