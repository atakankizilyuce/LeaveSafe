// Package update reports whether a newer release exists.
//
// LeaveSafe ships as a single file that nothing installs and nothing updates.
// A user who downloaded it once will run that copy for as long as it keeps
// working — which, for a program whose job is security, means running a version
// with a known flaw long after the flaw was fixed, with nothing to tell them.
//
// This asks GitHub once at startup and says what it found. It does not
// download, it does not replace the binary, and it does not run again while the
// program is up. Automatic updates would mean a security program silently
// replacing itself from the network, which is a larger trust decision than the
// user made when they downloaded a single file.
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

// DefaultReleasesURL is the GitHub API endpoint for the newest release.
const DefaultReleasesURL = "https://api.github.com/repos/atakankizilyuce/LeaveSafe/releases/latest"

// requestTimeout bounds the whole check. It runs at startup, off the path that
// brings the dashboard up, and a slow or unreachable GitHub must never be the
// reason an alarm takes longer to start watching.
const requestTimeout = 6 * time.Second

// Result describes what the check found.
type Result struct {
	// Available is true when the published release is newer than this build.
	Available bool
	// Latest is the published version, e.g. "v1.2.0".
	Latest string
	// URL is where to get it.
	URL string
}

// Checker queries a releases endpoint.
type Checker struct {
	// URL is the releases endpoint. Empty means DefaultReleasesURL.
	URL string
	// Client is the HTTP client to use. Nil means a client with a sane timeout.
	Client *http.Client
}

// release is the subset of the GitHub release payload this needs.
type release struct {
	TagName  string `json:"tag_name"`
	HTMLURL  string `json:"html_url"`
	Draft    bool   `json:"draft"`
	Prelease bool   `json:"prerelease"`
}

// Check reports whether a release newer than current exists.
//
// A development build — anything that is not a released version — is never
// reported as out of date: someone running a build from source did not get it
// from the releases page and does not want to be sent there.
func (c Checker) Check(ctx context.Context, current string) (Result, error) {
	if !isRelease(current) {
		return Result{}, nil
	}

	endpoint := c.URL
	if endpoint == "" {
		endpoint = DefaultReleasesURL
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub rejects unidentified API clients, and an honest agent string is
	// the least a program can do when it reaches out on the user's behalf.
	req.Header.Set("User-Agent", "LeaveSafe/"+current)

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("query releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("releases endpoint returned %s", resp.Status)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Result{}, fmt.Errorf("decode release: %w", err)
	}
	if rel.Draft || rel.Prelease || rel.TagName == "" {
		return Result{}, nil
	}

	if compareVersions(rel.TagName, current) <= 0 {
		return Result{}, nil
	}
	return Result{Available: true, Latest: rel.TagName, URL: rel.HTMLURL}, nil
}

// isRelease reports whether v looks like a released version rather than a
// development build.
func isRelease(v string) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" || v == "dev" {
		return false
	}
	// A release tag starts with a number. "dev", "" and anything a developer
	// stamped in by hand do not.
	_, err := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	return err == nil
}

// compareVersions orders two semver-ish version strings, returning a negative
// number if a sorts before b, zero if they are equal, and a positive number if
// a sorts after b.
//
// Only the numeric components are compared. Pre-release suffixes are ignored
// rather than ordered, because a release carrying one is skipped before this is
// reached, and guessing at an ordering that is never exercised would be a
// subtlety with no test behind it.
func compareVersions(a, b string) int {
	pa := versionParts(a)
	pb := versionParts(b)
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

// versionParts splits a version into its numeric components.
func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// Cut any pre-release or build suffix: 1.2.0-rc1 compares as 1.2.0.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}
