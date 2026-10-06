//go:build tray

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// createNoWindow keeps PowerShell from flashing a console window.
const createNoWindow = 0x08000000

// promptText asks for one line of text with the VB InputBox that ships with
// Windows PowerShell; ok is false if the user cancelled (InputBox returns "").
// Text reaches the script through the environment, never spliced into it.
func promptText(title, message, def string) (text string, ok bool, err error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		`Add-Type -AssemblyName Microsoft.VisualBasic; [Microsoft.VisualBasic.Interaction]::InputBox($env:PB_MESSAGE, $env:PB_TITLE, $env:PB_DEFAULT)`)
	cmd.Env = append(os.Environ(), "PB_TITLE="+title, "PB_MESSAGE="+message, "PB_DEFAULT="+def)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		return "", false, err
	}
	text = strings.TrimSpace(string(out))
	return text, text != "", nil
}
