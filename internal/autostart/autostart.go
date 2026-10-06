// Package autostart registers Patchbay to start when the user logs in: a
// LaunchAgent on macOS, a HKCU Run entry on Windows, and an XDG autostart
// desktop entry elsewhere. Registration is per user and needs no privileges.
package autostart

import (
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// ID names the LaunchAgent and matches the app's bundle identifier.
const ID = "io.github.robinbinu.patchbay"

// ErrNotInstalled means the running copy lives somewhere that won't exist at
// the next login (a mounted disk image or a macOS App Translocation path).
var ErrNotInstalled = errors.New("move Patchbay to Applications first")

// Enabled reports whether Patchbay is registered to start at login.
func Enabled() bool { return enabled() }

// Set registers (on) or unregisters (off) Patchbay to start at login, pointing
// at the copy that is running now.
func Set(on bool) error {
	if !on {
		return disable()
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	return enable(launchCommand(exe))
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if strings.Contains(exe, "/AppTranslocation/") || strings.HasPrefix(exe, "/Volumes/") {
		return "", ErrNotInstalled
	}
	return exe, nil
}

// launchCommand is what runs at login. Inside a macOS .app it opens the
// bundle through LaunchServices, so it starts like a double-click (and not a
// second time if it is already running); otherwise it runs `<exe> serve`.
func launchCommand(exe string) []string {
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		return []string{"/usr/bin/open", "-a", exe[:i+len(".app")]}
	}
	return []string{exe, "serve"}
}

func launchAgentPlist(args []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + ID + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range args {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", html.EscapeString(a))
	}
	b.WriteString(`	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`)
	return b.String()
}

func desktopEntry(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = desktopQuote(a)
	}
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Patchbay\n" +
		"Comment=One local endpoint for every model you have\n" +
		"Exec=" + strings.Join(quoted, " ") + "\n" +
		"Terminal=false\n" +
		"X-GNOME-Autostart-enabled=true\n"
}

// desktopQuote quotes an Exec argument per the Desktop Entry spec.
func desktopQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\`$;&|<>()*?#~=%") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`, `%`, `%%`)
	return `"` + r.Replace(s) + `"`
}

// windowsRunValue is the command line stored under HKCU\…\Run.
func windowsRunValue(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		parts[i] = a
	}
	return strings.Join(parts, " ")
}
