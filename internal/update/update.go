// Package update checks GitHub Releases for a newer Patchbay. It only reads
// the public releases list; nothing is downloaded or installed.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ReleasesURL lists the project's releases, newest first.
var ReleasesURL = "https://api.github.com/repos/Robinbinu/patchbay/releases?per_page=20"

// Release is a published version.
type Release struct {
	Tag string // e.g. "v0.2.0"
	URL string // its release page
}

// Check returns the newest release that is newer than current, and whether
// there is one. Users on a pre-release are offered pre-releases too; users on
// a stable version only stable ones.
func Check(ctx context.Context, client *http.Client, current string) (Release, bool, error) {
	cur, ok := parse(current)
	if !ok {
		return Release{}, false, fmt.Errorf("cannot compare development build %q", current)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleasesURL, nil)
	if err != nil {
		return Release{}, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "patchbay/"+current)
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, false, fmt.Errorf("GitHub releases: %s", resp.Status)
	}
	var list []struct {
		TagName    string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return Release{}, false, fmt.Errorf("GitHub releases: %w", err)
	}
	var best Release
	bestV := cur
	for _, r := range list {
		v, ok := parse(r.TagName)
		if !ok || r.Draft || (len(cur.pre) == 0 && (r.Prerelease || len(v.pre) > 0)) {
			continue
		}
		if compare(v, bestV) > 0 {
			best, bestV = Release{Tag: r.TagName, URL: r.HTMLURL}, v
		}
	}
	return best, best.Tag != "", nil
}

// DefaultClient is used for update checks: short timeout, no retries.
var DefaultClient = &http.Client{Timeout: 15 * time.Second}

type version struct {
	core [3]int
	pre  []string
}

// parse reads "v1.2.3" or "v1.2.3-beta.1" (the leading v is optional).
// Build metadata ("+…") is ignored, as semver says.
func parse(s string) (version, bool) {
	s = strings.TrimPrefix(s, "v")
	s, _, _ = strings.Cut(s, "+")
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var v version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		v.core[i] = n
	}
	if hasPre {
		if pre == "" {
			return version{}, false
		}
		v.pre = strings.Split(pre, ".")
	}
	return v, true
}

// compare orders versions by semver precedence: -1, 0 or 1.
func compare(a, b version) int {
	for i := range a.core {
		if a.core[i] != b.core[i] {
			return cmpInt(a.core[i], b.core[i])
		}
	}
	// A release outranks any of its pre-releases.
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := cmpIdent(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a.pre), len(b.pre))
}

// cmpIdent compares pre-release identifiers: numbers numerically and below
// words, words lexically.
func cmpIdent(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return cmpInt(an, bn)
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
