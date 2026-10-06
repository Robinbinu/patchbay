package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Robinbinu/patchbay/internal/proxy"
)

// proxyRunner starts and stops the HTTP server on demand, so the menu bar can
// turn the proxy off and on without quitting. Stops the user asks for are
// silent; a server that dies on its own is reported on Failed.
type proxyRunner struct {
	addr string
	h    http.Handler

	mu     sync.Mutex
	srv    *http.Server
	ln     net.Listener
	failed chan error
}

func newProxyRunner(addr string, h http.Handler) *proxyRunner {
	return &proxyRunner{addr: addr, h: h, failed: make(chan error, 1)}
}

// Start binds the address and serves in the background. It binds before
// returning so a taken port is reported to the caller, not lost in a goroutine.
// Starting a running proxy does nothing.
func (r *proxyRunner) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.srv != nil {
		return nil
	}
	ln, err := net.Listen("tcp", r.addr)
	if err != nil {
		return err
	}
	srv := proxy.NewServer(r.h)
	r.srv, r.ln = srv, ln
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		r.mu.Lock()
		if r.srv == srv {
			r.srv, r.ln = nil, nil
		}
		r.mu.Unlock()
		select {
		case r.failed <- err:
		default: // an earlier failure is still unread; one is enough to show
		}
	}()
	return nil
}

// Stop shuts the server down, giving in-flight requests a few seconds to
// finish before cutting long streams off.
func (r *proxyRunner) Stop() {
	r.mu.Lock()
	srv := r.srv
	r.srv, r.ln = nil, nil
	r.mu.Unlock()
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
	}
}

func (r *proxyRunner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.srv != nil
}

// Addr is the bound address while running (useful when listening on port 0).
func (r *proxyRunner) Addr() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ln == nil {
		return r.addr
	}
	return r.ln.Addr().String()
}

// Failed delivers errors from a server that stopped without being asked to.
func (r *proxyRunner) Failed() <-chan error { return r.failed }
