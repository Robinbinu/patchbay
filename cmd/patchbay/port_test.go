package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robinbinu/patchbay/internal/config"
)

// busyAddr returns an address held by a listener that is not Patchbay.
func busyAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func TestStartProxyMovesOffBusyPort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	busy := busyAddr(t)
	cfg := config.Default()
	cfg.SetListen(busy)
	r := newProxyRunner("", okHandler())
	defer r.Stop()

	moved, err := startProxy(cfg, r)
	if err != nil {
		t.Fatal(err)
	}
	if moved != busy {
		t.Errorf("moved = %q, want %q", moved, busy)
	}
	now := cfg.ListenAddr()
	if now == busy || portOf(now) <= portOf(busy) || portOf(now) > portOf(busy)+portSearch {
		t.Fatalf("new address %s is not a nearby free port after %s", now, busy)
	}
	if r.Addr() != now || get(t, now) != nil {
		t.Fatalf("runner not serving on %s (Addr %s)", now, r.Addr())
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.ListenAddr() != now {
		t.Errorf("new port not saved: config has %s, serving %s", saved.ListenAddr(), now)
	}
}

func TestStartProxyRefusesSecondPatchbay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"ok":true,"service":"patchbay"}`)
	}))
	defer other.Close()
	addr := other.Listener.Addr().String()
	cfg := config.Default()
	cfg.SetListen(addr)
	r := newProxyRunner("", okHandler())
	defer r.Stop()

	_, err := startProxy(cfg, r)
	if _, ok := err.(errAlreadyRunning); !ok {
		t.Fatalf("err = %v, want errAlreadyRunning", err)
	}
	if r.Running() || cfg.ListenAddr() != addr {
		t.Errorf("started a second proxy or changed the port (now %s)", cfg.ListenAddr())
	}
}

func TestStartProxyFreePortUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // free again: the configured address must be kept
	cfg := config.Default()
	cfg.SetListen(addr)
	r := newProxyRunner("", okHandler())
	defer r.Stop()
	if moved, err := startProxy(cfg, r); err != nil || moved != "" || cfg.ListenAddr() != addr {
		t.Fatalf("moved=%q err=%v addr=%s; want to stay on %s", moved, err, cfg.ListenAddr(), addr)
	}
}

func TestParsePort(t *testing.T) {
	for in, want := range map[string]int{"8788": 8788, " 9000 ": 9000, "65535": 65535} {
		if got, err := parsePort(in); err != nil || got != want {
			t.Errorf("parsePort(%q) = %d, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "abc", "80", "1023", "65536", "-1"} {
		if _, err := parsePort(bad); err == nil {
			t.Errorf("parsePort(%q) should fail", bad)
		}
	}
	if got := withPort("127.0.0.1:8787", 9000); got != "127.0.0.1:9000" {
		t.Errorf("withPort = %s", got)
	}
}
