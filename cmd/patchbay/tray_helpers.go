//go:build tray

package main

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
)

func contextBackground() context.Context { return context.Background() }

// clipboardCopy best-effort copies text using the platform clipboard tool.
func clipboardCopy(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("cmd", "/c", "clip")
	default:
		cmd = exec.Command("xclip", "-selection", "clipboard")
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
