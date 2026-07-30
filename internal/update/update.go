// Package update reports whether a newer release exists.
//
// LeaveSafe ships as a single file that nothing installs and nothing updates.
// A user who downloaded it once will run that copy for as long as it keeps
// working — which, for a program whose job is security, means running a version
// with a known flaw long after the flaw was fixed, with nothing to tell them.
//
// This asks GitHub and says what it found. It does not download, it does not
// replace the binary, and it does not act on what it learns. Automatic updates
// would mean a security program silently replacing itself from the network,
// which is a larger trust decision than the user made when they downloaded a
// single file.
//
// Everything the endpoint returns is treated as untrusted input. A version
// string from here is rendered on the user's phone, so a tag that does not look
// like a version and a URL that does not point at GitHub are discarded rather
// than displayed.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultReleasesURL lists this project's releases, newest first.
//
// The list is used rather than /releases/latest because that endpoint cannot
// express a channel: it never returns a prerelease, so a user who has opted into
// betas could never be told about one.
const DefaultReleasesURL = "https://api.github.com/repos/atakankizilyuce/LeaveSafe/releases?per_page=30"

// ReleasesPageURL is where to send someone when a release names no URL of its own.
const ReleasesPageURL = "https://github.com/atakankizilyuce/LeaveSafe/releases/latest"

// ChannelStable receives only full releases. It is the default: someone who has
// not asked for prereleases has asked for the opposite.
const ChannelStable = "stable"

// ChannelBeta receives prereleases as well as full ones. It means "also betas",
// not "only betas".
const ChannelBeta = "beta"

// requestTimeout bounds the whole check. It runs off the path that brings the
// dashboard up, and a slow or unreachable GitHub must never be the reason an
// alarm takes longer to start watching.
const requestTimeout = 6 * time.Second

// maxResponseBytes caps what will be read from the endpoint. A page of thirty
// releases is far smaller than this; anything larger is not a release list.
const maxResponseBytes = 1 << 20

// tagPattern is what a version tag is allowed to look like. This is a filter on
// data from the network that ends up on a phone screen, not a parser: a tag that
// does not match is discarded along with its release.
var tagPattern = regexp.MustCompile(`^v?\d+(\.\d+){0,3}(-[0-9A-Za-z.-]+)?$`)

// Result describes what the check found.
type Result struct {
	// Available is true when a published release is newer than this build.
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
	// Channel selects which releases count. Empty means ChannelStable.
	Channel string
}

// release is the subset of the GitHub release payload this needs.
type release struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// Check reports whether a release newer than current exists on the configured
// channel.
//
// A development build — anything that is not a released version — is never
// reported as out of date: someone running a build from source did not get it
// from the releases page and does not want to be sent there.
func (c Checker) Check(ctx context.Context, current string) (Result, error) {
	if !IsRelease(current) {
		return Result{}, nil
	}

	releases, err := c.fetch(ctx, current)
	if err != nil {
		return Result{}, err
	}

	// Pick the highest eligible version rather than the first one listed. The
	// endpoint orders by publication date, and a patch for an older line can be
	// published after a newer release, which would otherwise hide the newer one.
	var best *release
	for i := range releases {
		rel := &releases[i]
		if !c.eligible(rel) {
			continue
		}
		if best == nil || compareVersions(rel.TagName, best.TagName) > 0 {
			best = rel
		}
	}
	if best == nil || compareVersions(best.TagName, current) <= 0 {
		return Result{}, nil
	}

	return Result{
		Available: true,
		Latest:    best.TagName,
		URL:       releaseURL(best.HTMLURL),
	}, nil
}

// fetch retrieves the release list.
func (c Checker) fetch(ctx context.Context, current string) ([]release, error) {
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
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub rejects unidentified API clients, and an honest agent string is
	// the least a program can do when it reaches out on the user's behalf.
	req.Header.Set("User-Agent", "LeaveSafe/"+current)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("releases endpoint returned %s", resp.Status)
	}

	var releases []release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}
	return releases, nil
}

// eligible reports whether a release counts on this checker's channel.
func (c Checker) eligible(rel *release) bool {
	if rel.Draft || rel.TagName == "" {
		return false
	}
	if rel.Prerelease && c.Channel != ChannelBeta {
		return false
	}
	return tagPattern.MatchString(rel.TagName)
}

// releaseURL returns raw if it is a GitHub address, and the releases page
// otherwise.
//
// The value is rendered on the phone, so a link the endpoint invented is not
// worth passing on: anything other than github.com is replaced rather than
// shown.
func releaseURL(raw string) string {
	if raw == "" {
		return ReleasesPageURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" {
		return ReleasesPageURL
	}
	return raw
}

// IsRelease reports whether v looks like a released version rather than a
// development build.
//
// Exported because the dashboard has to explain itself: asked to check, it says
// "this is a development build" rather than "you are up to date", which would be
// a claim it has no basis for.
func IsRelease(v string) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" || v == "dev" {
		return false
	}
	// A release tag starts with a number. "dev", "" and anything a developer
	// stamped in by hand do not.
	_, err := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	return err == nil
}

// NormalizeChannel returns the channel name to use for c, defaulting unknown
// values to ChannelStable.
func NormalizeChannel(c string) string {
	if strings.EqualFold(strings.TrimSpace(c), ChannelBeta) {
		return ChannelBeta
	}
	return ChannelStable
}

// compareVersions orders two semver-ish version strings, returning a negative
// number if a sorts before b, zero if they are equal, and a positive number if
// a sorts after b.
//
// Numeric components are compared first. When those are equal, a version
// carrying a pre-release suffix sorts before the same version without one, so
// v1.0.4-beta is correctly older than v1.0.4 — which matters on the beta
// channel, where both can be in the same list.
//
// Two different suffixes at the same numeric version compare equal. Ordering
// -beta against -rc1 would need rules that nothing here exercises, and an
// untested subtlety is worse than a stated limit.
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

	preA := hasPrerelease(a)
	preB := hasPrerelease(b)
	switch {
	case preA && !preB:
		return -1
	case !preA && preB:
		return 1
	default:
		return 0
	}
}

// hasPrerelease reports whether v carries a pre-release or build suffix.
func hasPrerelease(v string) bool {
	return strings.ContainsAny(strings.TrimPrefix(strings.TrimSpace(v), "v"), "-+")
}

// versionParts splits a version into its numeric components.
func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// Cut any pre-release or build suffix: the numeric run is compared first,
	// and the suffix is weighed separately by compareVersions.
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
