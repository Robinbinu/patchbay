package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompare(t *testing.T) {
	// Each version is lower than the next (semver.org §11 example, plus ours).
	ordered := []string{
		"v0.1.0-beta.1", "v0.1.0-beta.2", "v0.1.0-beta.10", "v0.1.0-rc.1", "v0.1.0",
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta",
		"1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0",
	}
	for i := 0; i+1 < len(ordered); i++ {
		a, _ := parse(ordered[i])
		b, _ := parse(ordered[i+1])
		if compare(a, b) != -1 || compare(b, a) != 1 {
			t.Errorf("want %s < %s", ordered[i], ordered[i+1])
		}
	}
	a, _ := parse("v1.2.3+build.5")
	b, _ := parse("1.2.3")
	if compare(a, b) != 0 {
		t.Errorf("build metadata should not affect precedence")
	}
	for _, bad := range []string{"dev", "", "v1.2", "v1.2.x", "v1.2.3-"} {
		if _, ok := parse(bad); ok {
			t.Errorf("parse(%q) should fail", bad)
		}
	}
}

func serve(t *testing.T, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("GitHub requires a User-Agent")
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	old := ReleasesURL
	ReleasesURL = srv.URL
	t.Cleanup(func() { ReleasesURL = old })
}

const releases = `[
  {"tag_name": "v0.3.0-beta.1", "html_url": "https://x/v0.3.0-beta.1", "prerelease": true},
  {"tag_name": "v0.2.0", "html_url": "https://x/v0.2.0"},
  {"tag_name": "v0.4.0", "html_url": "https://x/v0.4.0", "draft": true},
  {"tag_name": "nightly", "html_url": "https://x/nightly"},
  {"tag_name": "v0.1.0-beta.1", "html_url": "https://x/v0.1.0-beta.1", "prerelease": true}
]`

func TestCheck(t *testing.T) {
	serve(t, releases)
	cases := []struct {
		current, want string
	}{
		{"v0.1.0-beta.1", "v0.3.0-beta.1"}, // beta users get betas
		{"v0.1.0", "v0.2.0"},               // stable users get stable only
		{"v0.2.0", ""},
		{"v0.3.0-beta.1", ""},
	}
	for _, c := range cases {
		r, newer, err := Check(context.Background(), http.DefaultClient, c.current)
		if err != nil {
			t.Fatalf("%s: %v", c.current, err)
		}
		if newer != (c.want != "") || r.Tag != c.want {
			t.Errorf("Check(%s) = %q, %v; want %q", c.current, r.Tag, newer, c.want)
		}
		if newer && r.URL != "https://x/"+c.want {
			t.Errorf("Check(%s) URL = %q", c.current, r.URL)
		}
	}
}

func TestCheckErrors(t *testing.T) {
	if _, _, err := Check(context.Background(), http.DefaultClient, "dev"); err == nil {
		t.Error("development builds should not be compared")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()
	old := ReleasesURL
	ReleasesURL = srv.URL
	defer func() { ReleasesURL = old }()
	if _, _, err := Check(context.Background(), http.DefaultClient, "v0.1.0"); err == nil {
		t.Error("a non-200 response should be an error")
	}
}
