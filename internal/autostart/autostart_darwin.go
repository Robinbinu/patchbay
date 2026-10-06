package autostart

import (
	"errors"
	"os"
	"path/filepath"
)

func agentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", ID+".plist"), nil
}

func enabled() bool {
	p, err := agentPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// enable writes the LaunchAgent; launchd loads it at the next login. It is not
// loaded now, which would start a second copy alongside the running one.
func enable(args []string) error {
	p, err := agentPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(launchAgentPlist(args)), 0o644)
}

func disable() error {
	p, err := agentPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
