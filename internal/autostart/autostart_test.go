package autostart

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestLaunchCommand(t *testing.T) {
	cases := []struct {
		exe  string
		want []string
	}{
		{"/Applications/Patchbay.app/Contents/MacOS/Patchbay",
			[]string{"/usr/bin/open", "-a", "/Applications/Patchbay.app"}},
		{"/usr/local/bin/patchbay", []string{"/usr/local/bin/patchbay", "serve"}},
		{`C:\Users\me\Patchbay\Patchbay.exe`, []string{`C:\Users\me\Patchbay\Patchbay.exe`, "serve"}},
	}
	for _, c := range cases {
		if got := launchCommand(c.exe); !reflect.DeepEqual(got, c.want) {
			t.Errorf("launchCommand(%q) = %q, want %q", c.exe, got, c.want)
		}
	}
}

func TestLaunchAgentPlist(t *testing.T) {
	got := launchAgentPlist([]string{"/Users/a & b/patchbay", "serve"})
	for _, want := range []string{
		"<string>" + ID + "</string>",
		"<string>/Users/a &amp; b/patchbay</string>",
		"<string>serve</string>",
		"<key>RunAtLoad</key>\n\t<true/>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plist missing %q:\n%s", want, got)
		}
	}
}

func TestDesktopEntryQuotesExec(t *testing.T) {
	got := desktopEntry([]string{"/home/me/my apps/patchbay", "serve"})
	if !strings.Contains(got, "Exec=\"/home/me/my apps/patchbay\" serve\n") {
		t.Errorf("unexpected Exec line:\n%s", got)
	}
	if got := desktopEntry([]string{"/usr/bin/patchbay", "serve"}); !strings.Contains(got, "Exec=/usr/bin/patchbay serve\n") {
		t.Errorf("unexpected Exec line:\n%s", got)
	}
}

func TestWindowsRunValue(t *testing.T) {
	got := windowsRunValue([]string{`C:\Program Files\Patchbay\Patchbay.exe`, "serve"})
	if want := `"C:\Program Files\Patchbay\Patchbay.exe" serve`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSetRoundTrip toggles the real registration inside a throwaway home.
func TestSetRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("writes to the real HKCU Run key")
	}
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", "")
	if Enabled() {
		t.Fatal("enabled in a fresh home")
	}
	if err := Set(true); err != nil {
		t.Fatal(err)
	}
	if !Enabled() {
		t.Fatal("not enabled after Set(true)")
	}
	if err := Set(false); err != nil {
		t.Fatal(err)
	}
	if Enabled() {
		t.Fatal("still enabled after Set(false)")
	}
	if err := Set(false); err != nil {
		t.Fatalf("disabling twice: %v", err)
	}
}

// setHome points the user's home at dir for the test: HOME on macOS and
// Linux, USERPROFILE on Windows (where os.UserHomeDir ignores HOME).
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
