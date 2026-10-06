//go:build tray && !darwin && !windows

package main

import (
	"errors"
	"os/exec"
	"strings"
)

// promptText asks for one line of text with zenity (GNOME) or kdialog (KDE);
// ok is false if the user cancelled.
func promptText(title, message, def string) (text string, ok bool, err error) {
	var cmd *exec.Cmd
	switch {
	case hasCommand("zenity"):
		cmd = exec.Command("zenity", "--entry", "--title", title, "--text", message, "--entry-text", def)
	case hasCommand("kdialog"):
		cmd = exec.Command("kdialog", "--title", title, "--inputbox", message, def)
	default:
		return "", false, errors.New("install zenity or kdialog, or run `patchbay port <number>`")
	}
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return "", false, nil // Cancel exits 1
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(out)), true, nil
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
