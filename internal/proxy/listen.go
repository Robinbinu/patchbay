package proxy

import (
	"net/http"
	"time"
)

// NewServer returns an http.Server for the proxy with timeouts suited to long
// model streams. The caller binds and serves it, so it can also shut it down.
func NewServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:      h,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 0, // streaming responses may run for minutes
		IdleTimeout:  120 * time.Second,
	}
}
