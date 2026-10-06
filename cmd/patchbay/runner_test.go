package main

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") })
}

func get(t *testing.T, addr string) error {
	t.Helper()
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://" + addr + "/")
	if err == nil {
		resp.Body.Close()
	}
	return err
}

func TestRunnerStopStart(t *testing.T) {
	r := newProxyRunner("127.0.0.1:0", okHandler())
	if err := r.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	addr := r.Addr()
	if !r.Running() || get(t, addr) != nil {
		t.Fatalf("proxy should serve after Start")
	}

	r.Stop()
	if r.Running() {
		t.Fatalf("Running after Stop")
	}
	if get(t, addr) == nil {
		t.Fatalf("proxy still serving after Stop")
	}
	select {
	case err := <-r.Failed():
		t.Fatalf("a requested stop must not report a failure, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	r.addr = addr // restart on the same port, as the menu does
	if err := r.Start(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer r.Stop()
	if get(t, addr) != nil {
		t.Fatalf("proxy should serve after restart")
	}
}

func TestRunnerStartReportsTakenPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	r := newProxyRunner(ln.Addr().String(), okHandler())
	if err := r.Start(); err == nil {
		r.Stop()
		t.Fatalf("Start on a taken port should fail")
	}
	if r.Running() {
		t.Fatalf("Running after a failed Start")
	}
}

func TestRunnerStartTwiceIsNoop(t *testing.T) {
	r := newProxyRunner("127.0.0.1:0", okHandler())
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	addr := r.Addr()
	if err := r.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if r.Addr() != addr {
		t.Fatalf("second Start rebound: %s -> %s", addr, r.Addr())
	}
}
