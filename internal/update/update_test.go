package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// releaseServer serves a release list as the GitHub endpoint does.
func releaseServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// list wraps release objects in the array the endpoint returns.
func list(entries ...string) string {
	return "[" + strings.Join(entries, ",") + "]"
}

func TestReportsANewerRelease(t *testing.T) {
	srv := releaseServer(t, http.StatusOK,
		list(`{"tag_name":"v1.4.0","html_url":"https://github.com/atakankizilyuce/LeaveSafe/releases/tag/v1.4.0"}`))

	got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.2.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !got.Available {
		t.Fatal("a newer release was not reported")
	}
	if got.Latest != "v1.4.0" {
		t.Errorf("Latest = %q, want v1.4.0", got.Latest)
	}
	if got.URL != "https://github.com/atakankizilyuce/LeaveSafe/releases/tag/v1.4.0" {
		t.Errorf("URL = %q, want the release page", got.URL)
	}
}

func TestSaysNothingWhenCurrent(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, list(`{"tag_name":"v1.2.0"}`))

	got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.2.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Available {
		t.Error("the running version was reported as out of date")
	}
}

// Running a build newer than the newest release is what a maintainer does, and
// being told to downgrade would be nonsense.
func TestSaysNothingWhenAhead(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, list(`{"tag_name":"v1.2.0"}`))

	got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.3.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Available {
		t.Error("an older release was reported as an update")
	}
}

// Someone running a build from source did not get it from the releases page and
// does not want to be sent there — and the check should not even fire.
func TestDevelopmentBuildsAreNeverChecked(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(list(`{"tag_name":"v9.9.9"}`)))
	}))
	defer srv.Close()

	for _, version := range []string{"dev", "", "some-branch"} {
		got, err := Checker{URL: srv.URL}.Check(context.Background(), version)
		if err != nil {
			t.Fatalf("Check(%q): %v", version, err)
		}
		if got.Available {
			t.Errorf("version %q was told to update", version)
		}
	}
	if hits != 0 {
		t.Errorf("the endpoint was queried %d times for development builds", hits)
	}
}

func TestDraftsAreIgnoredOnEveryChannel(t *testing.T) {
	for _, channel := range []string{ChannelStable, ChannelBeta} {
		srv := releaseServer(t, http.StatusOK, list(`{"tag_name":"v2.0.0","draft":true}`))
		got, err := Checker{URL: srv.URL, Channel: channel}.Check(context.Background(), "v1.0.0")
		if err != nil {
			t.Fatalf("Check(%s): %v", channel, err)
		}
		if got.Available {
			t.Errorf("a draft was offered as an update on the %s channel", channel)
		}
	}
}

func TestReleasesWithoutATagAreIgnored(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, list(`{"tag_name":""}`))

	got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Available {
		t.Error("a release with no tag was offered as an update")
	}
}

// The stable channel is the default, and it is what every existing config
// silently selects.
func TestStableChannelSkipsPrereleases(t *testing.T) {
	body := list(
		`{"tag_name":"v2.0.0-beta","prerelease":true}`,
		`{"tag_name":"v1.5.0"}`,
	)
	for _, channel := range []string{"", ChannelStable, "nonsense"} {
		srv := releaseServer(t, http.StatusOK, body)
		got, err := Checker{URL: srv.URL, Channel: NormalizeChannel(channel)}.Check(context.Background(), "v1.0.0")
		if err != nil {
			t.Fatalf("Check(%q): %v", channel, err)
		}
		if !got.Available || got.Latest != "v1.5.0" {
			t.Errorf("channel %q reported %+v, want v1.5.0", channel, got)
		}
	}
}

func TestBetaChannelSeesPrereleases(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, list(
		`{"tag_name":"v2.0.0-beta","prerelease":true}`,
		`{"tag_name":"v1.5.0"}`,
	))

	got, err := Checker{URL: srv.URL, Channel: ChannelBeta}.Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !got.Available || got.Latest != "v2.0.0-beta" {
		t.Errorf("got %+v, want v2.0.0-beta", got)
	}
}

// Beta means "also betas", not "only betas": with no prerelease published, a
// beta user still hears about the newest full release.
func TestBetaChannelFallsThroughToStable(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, list(`{"tag_name":"v1.5.0"}`))

	got, err := Checker{URL: srv.URL, Channel: ChannelBeta}.Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !got.Available || got.Latest != "v1.5.0" {
		t.Errorf("got %+v, want v1.5.0", got)
	}
}

// This is the case the old single-object endpoint could not express: someone on
// v1.0.4-beta when v1.0.4 proper has shipped is running the older build.
func TestStableSupersedesTheMatchingPrerelease(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, list(
		`{"tag_name":"v1.0.4"}`,
		`{"tag_name":"v1.0.4-beta","prerelease":true}`,
	))

	got, err := Checker{URL: srv.URL, Channel: ChannelBeta}.Check(context.Background(), "v1.0.4-beta")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !got.Available || got.Latest != "v1.0.4" {
		t.Errorf("got %+v, want v1.0.4", got)
	}
}

// Today's repository state: every release is a prerelease, so a stable user is
// correctly told nothing at all.
func TestStableChannelWithOnlyPrereleasesReportsNothing(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, list(
		`{"tag_name":"v1.0.4-beta","prerelease":true}`,
		`{"tag_name":"v1.0.3-beta","prerelease":true}`,
	))

	got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Available {
		t.Errorf("a stable user was offered %q", got.Latest)
	}
}

func TestEmptyListReportsNothing(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, `[]`)

	got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Available {
		t.Error("an empty release list produced an update")
	}
}

// The endpoint orders by publication date, so a patch for an older line can be
// published after a newer release. Taking the first entry would hide the newer one.
func TestPicksTheHighestVersionNotTheNewestPublished(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, list(
		`{"tag_name":"v1.2.5"}`,
		`{"tag_name":"v1.3.0"}`,
	))

	got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.2.4")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Latest != "v1.3.0" {
		t.Errorf("Latest = %q, want v1.3.0", got.Latest)
	}
}

// A version string from the network is rendered on the user's phone. Anything
// that is not a version is discarded along with its release.
func TestTagsThatAreNotVersionsAreRejected(t *testing.T) {
	for _, tag := range []string{
		`nightly`,
		`v1.0.0 <script>alert(1)</script>`,
		`../../etc/passwd`,
		`v1.0.0\nX-Injected: yes`,
		`</div><img src=x onerror=alert(1)>`,
		`v1.0.0; rm -rf /`,
	} {
		srv := releaseServer(t, http.StatusOK, list(fmt.Sprintf(`{"tag_name":%q}`, tag)))
		got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.0.0")
		if err != nil {
			t.Fatalf("Check(%q): %v", tag, err)
		}
		if got.Available {
			t.Errorf("tag %q was accepted and would reach the phone", tag)
		}
	}
}

// The URL is a link the phone will offer. A release that names somewhere other
// than GitHub gets the releases page instead.
func TestOffSiteURLsAreReplaced(t *testing.T) {
	for _, raw := range []string{
		`http://github.com/x`,
		`https://evil.invalid/x`,
		`https://github.com.evil.invalid/x`,
		`javascript:alert(1)`,
		``,
	} {
		srv := releaseServer(t, http.StatusOK,
			list(fmt.Sprintf(`{"tag_name":"v2.0.0","html_url":%q}`, raw)))
		got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.0.0")
		if err != nil {
			t.Fatalf("Check(%q): %v", raw, err)
		}
		if !got.Available {
			t.Fatalf("html_url %q suppressed the release itself", raw)
		}
		if got.URL != ReleasesPageURL {
			t.Errorf("html_url %q survived as %q", raw, got.URL)
		}
	}
}

func TestServerErrorsAreReported(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		srv := releaseServer(t, status, `{"message":"nope"}`)
		if _, err := (Checker{URL: srv.URL}).Check(context.Background(), "v1.0.0"); err == nil {
			t.Errorf("status %d from the releases endpoint returned no error", status)
		}
	}
}

func TestMalformedResponseIsReported(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, `not json`)

	if _, err := (Checker{URL: srv.URL}).Check(context.Background(), "v1.0.0"); err == nil {
		t.Error("an unparsable response returned no error")
	}
}

// A response larger than the cap must fail rather than be read to exhaustion.
func TestOversizedResponseIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"tag_name":"v2.0.0","html_url":"`))
		filler := strings.Repeat("a", 4096)
		for written := 0; written < maxResponseBytes; written += len(filler) {
			if _, err := w.Write([]byte(filler)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	if _, err := (Checker{URL: srv.URL}).Check(context.Background(), "v1.0.0"); err == nil {
		t.Error("a response past the size cap returned no error")
	}
}

func TestUnreachableEndpointIsReported(t *testing.T) {
	// A port nothing is listening on: the check must fail rather than hang.
	if _, err := (Checker{URL: "http://127.0.0.1:1"}).Check(context.Background(), "v1.0.0"); err == nil {
		t.Error("an unreachable endpoint returned no error")
	}
}

func TestNormalizeChannel(t *testing.T) {
	cases := map[string]string{
		"":         ChannelStable,
		"stable":   ChannelStable,
		"beta":     ChannelBeta,
		"BETA":     ChannelBeta,
		" beta ":   ChannelBeta,
		"nonsense": ChannelStable,
	}
	for in, want := range cases {
		if got := NormalizeChannel(in); got != want {
			t.Errorf("NormalizeChannel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want string // "before", "same" or "after"
	}{
		{"v1.2.0", "v1.2.0", "same"},
		{"1.2.0", "v1.2.0", "same"},
		{"v1.2.1", "v1.2.0", "after"},
		{"v1.3.0", "v1.2.9", "after"},
		{"v2.0.0", "v1.99.99", "after"},
		{"v1.2.0", "v1.2.1", "before"},
		{"v1.2", "v1.2.0", "same"},
		{"v1.2.0", "v1.2", "same"},
		{"v1.10.0", "v1.9.0", "after"},
		// A pre-release sorts before the release it leads up to.
		{"v1.2.0-rc1", "v1.2.0", "before"},
		{"v1.2.0", "v1.2.0-rc1", "after"},
		{"v1.0.4-beta", "v1.0.4", "before"},
		{"v1.0.4-beta", "v1.0.5-beta", "before"},
		{"v1.1.0-beta", "v1.0.9", "after"},
		// Two different suffixes at the same version are not ordered. This is a
		// stated limit, not an oversight.
		{"v1.2.0-beta", "v1.2.0-rc1", "same"},
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		var label string
		switch {
		case got < 0:
			label = "before"
		case got > 0:
			label = "after"
		default:
			label = "same"
		}
		if label != c.want {
			t.Errorf("compareVersions(%q, %q) sorts %s, want %s", c.a, c.b, label, c.want)
		}
	}
}

// A double-digit minor sorting below a single-digit one is the classic string
// comparison bug, and it would tell every user of v1.10 to downgrade to v1.9.
func TestNumericComparisonNotLexicographic(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, list(`{"tag_name":"v1.9.0"}`))

	got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.10.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Available {
		t.Error("v1.9.0 was offered as an update to v1.10.0")
	}
}
