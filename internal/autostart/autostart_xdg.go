//go:build !darwin && !windows

package autostart

import (
	"errors"
	"os"
	"path/filepath"
)

// entryPath follows the XDG Autostart spec: $XDG_CONFIG_HOME/autostart.
func entryPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "patchbay.desktop"), nil
}

func enabled() bool {
	p, err := entryPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func enable(args []string) error {
	p, err := entryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(desktopEntry(args)), 0o644)
}

func disable() error {
	p, err := entryPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
