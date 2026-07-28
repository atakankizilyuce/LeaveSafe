package location

import (
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSharedFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// TestParseIPLookup runs against a response ipapi.co really returned, so a
// change to the field names it uses is caught here rather than by a user
// looking at an empty panel. See testdata/PROVENANCE.md.
func TestParseIPLookup(t *testing.T) {
	fix, err := parseIPLookup(readSharedFixture(t, "ipapi_co.json"))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if math.Abs(fix.Latitude-41.0931) > 0.0001 {
		t.Errorf("latitude = %f, want 41.0931", fix.Latitude)
	}
	if math.Abs(fix.Longitude-28.802) > 0.0001 {
		t.Errorf("longitude = %f, want 28.802", fix.Longitude)
	}
	if fix.Source != SourceIP {
		t.Errorf("source = %q, want %q", fix.Source, SourceIP)
	}
	if fix.AccuracyM != ipAccuracyM {
		t.Errorf("accuracy = %f, want %f", fix.AccuracyM, float64(ipAccuracyM))
	}
	if fix.Label != "Basaksehir, Turkey" {
		t.Errorf("label = %q, want %q", fix.Label, "Basaksehir, Turkey")
	}
}

// TestParseIPLookup_IpwhoIsShape covers the other widely used endpoint, which
// names the country field differently. Both are accepted so geolocate_url can
// be repointed without a code change.
func TestParseIPLookup_IpwhoIsShape(t *testing.T) {
	body := []byte(`{"ip":"203.0.113.7","success":true,"country":"Türkiye","country_code":"TR",
		"city":"Istanbul","latitude":41.0138411,"longitude":28.949657}`)

	fix, err := parseIPLookup(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if fix.Label != "Istanbul, Türkiye" {
		t.Errorf("label = %q, want %q", fix.Label, "Istanbul, Türkiye")
	}
}

// TestParseIPLookup_MissingCoordinates is the case that would otherwise put the
// machine on null island: a rate-limited or unroutable address comes back as a
// perfectly well-formed document with the coordinates simply absent.
func TestParseIPLookup_MissingCoordinates(t *testing.T) {
	body := []byte(`{"ip":"203.0.113.7","city":"","country_name":""}`)

	if _, err := parseIPLookup(body); err == nil {
		t.Error("expected an error when the response carries no coordinates")
	}
}

// TestParseIPLookup_ZeroIsAValidCoordinate guards against fixing the above by
// treating zero as missing. 0,0 is in the Gulf of Guinea, but 0 latitude is a
// real value at any longitude.
func TestParseIPLookup_ZeroIsAValidCoordinate(t *testing.T) {
	body := []byte(`{"latitude":0,"longitude":6.5,"city":"Sao Tome","country_name":"Sao Tome"}`)

	fix, err := parseIPLookup(body)
	if err != nil {
		t.Fatalf("a zero latitude was rejected as missing: %v", err)
	}
	if fix.Latitude != 0 || math.Abs(fix.Longitude-6.5) > 0.0001 {
		t.Errorf("got %f,%f want 0,6.5", fix.Latitude, fix.Longitude)
	}
}

func TestParseIPLookup_ServiceError(t *testing.T) {
	body := []byte(`{"error":true,"reason":"RateLimited"}`)

	_, err := parseIPLookup(body)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "RateLimited") {
		t.Errorf("error = %q, want it to carry the service's reason", got)
	}
}

func TestParseIPLookup_Garbage(t *testing.T) {
	if _, err := parseIPLookup([]byte("<html>not json</html>")); err == nil {
		t.Error("expected an error for a non-JSON response")
	}
}

func TestParseGeolocateResponse(t *testing.T) {
	body := []byte(`{"location":{"lat":41.0138,"lng":28.9496},"accuracy":27.5}`)

	fix, err := parseGeolocateResponse(body, http.StatusOK, 6)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if fix.Source != SourceWiFi {
		t.Errorf("source = %q, want %q", fix.Source, SourceWiFi)
	}
	if math.Abs(fix.AccuracyM-27.5) > 0.001 {
		t.Errorf("accuracy = %f, want 27.5", fix.AccuracyM)
	}
	if fix.Label != "6 access points" {
		t.Errorf("label = %q, want %q", fix.Label, "6 access points")
	}
}

// TestParseGeolocateResponse_MissingAccuracy pins the deliberate choice not to
// default to zero. A zero radius renders as pinpoint certainty, which is the
// opposite of what an absent accuracy figure means.
func TestParseGeolocateResponse_MissingAccuracy(t *testing.T) {
	body := []byte(`{"location":{"lat":41.0138,"lng":28.9496}}`)

	fix, err := parseGeolocateResponse(body, http.StatusOK, 3)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if fix.AccuracyM <= 0 {
		t.Errorf("accuracy = %f, want a deliberately wide fallback rather than certainty", fix.AccuracyM)
	}
}

func TestParseGeolocateResponse_ServiceError(t *testing.T) {
	body := []byte(`{"error":{"code":403,"message":"Geolocation API has not been used in project"}}`)

	_, err := parseGeolocateResponse(body, http.StatusForbidden, 4)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Geolocation API has not been used") {
		t.Errorf("error = %q, want it to carry the service's message", err)
	}
}

func TestParseGeolocateResponse_NoLocation(t *testing.T) {
	if _, err := parseGeolocateResponse([]byte(`{}`), http.StatusOK, 2); err == nil {
		t.Error("expected an error when the response carries no location")
	}
}
