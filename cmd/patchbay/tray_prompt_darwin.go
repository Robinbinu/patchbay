//go:build tray

package main

import (
	"errors"
	"os/exec"
	"strings"
)

// promptScript shows a native input dialog. Text arrives as arguments, never
// spliced into the script.
const promptScript = `on run argv
	activate
	display dialog (item 2 of argv) default answer (item 3 of argv) with title (item 1 of argv) buttons {"Cancel", "OK"} default button "OK" cancel button "Cancel"
	return text returned of result
end run`

// promptText asks for one line of text; ok is false if the user cancelled.
func promptText(title, message, def string) (text string, ok bool, err error) {
	out, err := exec.Command("osascript", "-e", promptScript, title, message, def).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return "", false, nil // Cancel exits non-zero (error -128)
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(out)), true, nil
}
