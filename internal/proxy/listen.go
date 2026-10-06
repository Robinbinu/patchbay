package proxy

import (
	"net/http"
	"time"
)

// ListenAndServe starts the proxy with timeouts suited to long model streams.
func ListenAndServe(addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      h,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 0, // streaming responses may run for minutes
		IdleTimeout:  120 * time.Second,
	}
	return srv.ListenAndServe()
}
