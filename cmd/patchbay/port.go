package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robinbinu/patchbay/internal/config"
)

// portSearch is how many ports above a taken one Patchbay tries.
const portSearch = 20

// errAlreadyRunning means another Patchbay already serves on the address, so
// starting a second one would only split clients between two proxies.
type errAlreadyRunning string

func (e errAlreadyRunning) Error() string {
	return "Patchbay is already running on http://" + string(e)
}

// startProxy starts the runner on the configured address. If that fails and
// the address isn't another Patchbay, it moves to the next free port and saves
// it, so tools pointed at Patchbay keep one stable URL from then on. moved is
// the address given up, or "" if none.
func startProxy(cfg *config.Config, runner *proxyRunner) (moved string, err error) {
	addr := cfg.ListenAddr()
	runner.SetAddr(addr)
	err = runner.Start()
	if err == nil {
		return "", nil
	}
	if patchbayAt(addr) {
		return "", errAlreadyRunning(addr)
	}
	free, ferr := nextFreeAddr(addr, portSearch)
	if ferr != nil {
		return "", err // nothing better nearby; report the original failure
	}
	runner.SetAddr(free)
	if serr := runner.Start(); serr != nil {
		return "", err
	}
	cfg.SetListen(free)
	if serr := cfg.Save(); serr != nil {
		return addr, fmt.Errorf("serving on %s, but saving the new port failed: %w", free, serr)
	}
	return addr, nil
}

// patchbayAt reports whether a Patchbay answers /healthz on addr.
func patchbayAt(addr string) bool {
	c := http.Client{Timeout: time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var h struct{ Service string }
	return json.NewDecoder(resp.Body).Decode(&h) == nil && h.Service == "patchbay"
}

// nextFreeAddr returns the first address after addr's port, on the same host,
// that can be bound, trying up to n ports.
func nextFreeAddr(addr string, n int) (string, error) {
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return "", err
	}
	for next := port + 1; next <= port+n && next <= 65535; next++ {
		cand := net.JoinHostPort(host, strconv.Itoa(next))
		if portFree(cand) {
			return cand, nil
		}
	}
	return "", fmt.Errorf("no free port in %d–%d", port+1, port+n)
}

// portFree reports whether addr can be bound right now.
func portFree(addr string) bool {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// withPort replaces the port of a host:port address.
func withPort(addr string, port int) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// portOf returns the port of a host:port address, or 0.
func portOf(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(p)
	return n
}

// parsePort reads a user-entered port. Ports below 1024 need admin rights on
// macOS and Linux, so they are refused rather than failing later.
func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, errors.New("enter a number")
	}
	if n < 1024 || n > 65535 {
		return 0, errors.New("use a port from 1024 to 65535")
	}
	return n, nil
}
