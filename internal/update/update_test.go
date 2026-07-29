package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func releaseServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReportsANewerRelease(t *testing.T) {
	srv := releaseServer(t, http.StatusOK,
		`{"tag_name":"v1.4.0","html_url":"https://example.invalid/v1.4.0"}`)

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
	if got.URL != "https://example.invalid/v1.4.0" {
		t.Errorf("URL = %q, want the release page", got.URL)
	}
}

func TestSaysNothingWhenCurrent(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, `{"tag_name":"v1.2.0"}`)

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
	srv := releaseServer(t, http.StatusOK, `{"tag_name":"v1.2.0"}`)

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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
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

func TestDraftsAndPrereleasesAreIgnored(t *testing.T) {
	for _, body := range []string{
		`{"tag_name":"v2.0.0","draft":true}`,
		`{"tag_name":"v2.0.0","prerelease":true}`,
		`{"tag_name":""}`,
	} {
		srv := releaseServer(t, http.StatusOK, body)
		got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.0.0")
		if err != nil {
			t.Fatalf("Check(%s): %v", body, err)
		}
		if got.Available {
			t.Errorf("release %s was offered as an update", body)
		}
	}
}

func TestServerErrorsAreReported(t *testing.T) {
	srv := releaseServer(t, http.StatusForbidden, `{"message":"rate limited"}`)

	if _, err := (Checker{URL: srv.URL}).Check(context.Background(), "v1.0.0"); err == nil {
		t.Error("a 403 from the releases endpoint returned no error")
	}
}

func TestMalformedResponseIsReported(t *testing.T) {
	srv := releaseServer(t, http.StatusOK, `not json`)

	if _, err := (Checker{URL: srv.URL}).Check(context.Background(), "v1.0.0"); err == nil {
		t.Error("an unparsable response returned no error")
	}
}

func TestUnreachableEndpointIsReported(t *testing.T) {
	// A port nothing is listening on: the check must fail rather than hang.
	if _, err := (Checker{URL: "http://127.0.0.1:1"}).Check(context.Background(), "v1.0.0"); err == nil {
		t.Error("an unreachable endpoint returned no error")
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
		// A pre-release suffix is dropped rather than ordered.
		{"v1.2.0-rc1", "v1.2.0", "same"},
		{"v1.10.0", "v1.9.0", "after"},
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
	srv := releaseServer(t, http.StatusOK, `{"tag_name":"v1.9.0"}`)

	got, err := Checker{URL: srv.URL}.Check(context.Background(), "v1.10.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Available {
		t.Error("v1.9.0 was offered as an update to v1.10.0")
	}
}
