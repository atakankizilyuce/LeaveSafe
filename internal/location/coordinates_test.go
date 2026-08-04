package location

import (
	"encoding/json"
	"strings"
	"testing"
)

// Everything in this file is about the same thing: a position source is a party
// outside this program, and the panel it feeds is the one that tells the owner
// where their laptop is. A source that answers with something that is not a
// place has to be refused rather than repeated.

// Coordinates arrive from a service this program did not write, over a
// connection to an endpoint the config can point anywhere. A point that is not
// on the globe reaches the tracker, spreads into the distance-moved figure, and
// settles there — so the panel would be stating a position nobody measured.
func TestCoordinatesOutsideTheGlobeAreRefused(t *testing.T) {
	bodies := map[string]string{
		"latitude past the pole":  `{"latitude": 91.0, "longitude": 10.0}`,
		"latitude below it":       `{"latitude": -90.5, "longitude": 10.0}`,
		"longitude past the line": `{"latitude": 10.0, "longitude": 180.1}`,
		"longitude below it":      `{"latitude": 10.0, "longitude": -181.0}`,
	}
	for what, body := range bodies {
		if fix, err := parseIPLookup([]byte(body)); err == nil {
			t.Errorf("%s was accepted as a position: %+v", what, fix)
		}
	}
}

// The Wi-Fi endpoint is configurable, so this is not always the service the
// default names. Its answer is held to the same range.
func TestGeolocatedCoordinatesOutsideTheGlobeAreRefused(t *testing.T) {
	bodies := map[string]string{
		"latitude past the pole":  `{"location": {"lat": 90.5, "lng": 10.0}, "accuracy": 30}`,
		"longitude past the line": `{"location": {"lat": 10.0, "lng": -181.0}, "accuracy": 30}`,
	}
	for what, body := range bodies {
		if fix, err := parseGeolocateResponse([]byte(body), 200, 4); err == nil {
			t.Errorf("%s was accepted as a position: %+v", what, fix)
		}
	}
}

// And an ordinary answer still has to come through, or turning the feature on
// would stop producing a position at all.
func TestAnOrdinaryPositionIsStillAccepted(t *testing.T) {
	fix, err := parseIPLookup([]byte(
		`{"latitude": 41.0082, "longitude": 28.9784, "city": "Istanbul", "country_name": "Turkey"}`))
	if err != nil {
		t.Fatalf("parseIPLookup: %v", err)
	}
	if fix.Latitude != 41.0082 || fix.Longitude != 28.9784 {
		t.Errorf("parsed %v, %v", fix.Latitude, fix.Longitude)
	}
	if fix.Label != "Istanbul, Turkey" {
		t.Errorf("label is %q, want %q", fix.Label, "Istanbul, Turkey")
	}
}

// The place name is repeated onto the owner's phone as a statement about where
// their laptop is, so it is filtered rather than passed through: control
// characters go, and so does anything past the length a place name has.
func TestThePlaceNameIsFiltered(t *testing.T) {
	// A NUL and an ANSI escape, encoded the way a service would have to send
	// them: a literal control byte is not valid JSON. The NUL sits inside the
	// name rather than at the end, because trimming the ends is the easy half.
	city, err := json.Marshal("New \x00York\x1b[31m")
	if err != nil {
		t.Fatalf("encode the city name: %v", err)
	}
	long := strings.Repeat("a", maxPlaceNameRunes+40)
	body := `{"latitude": 1.0, "longitude": 2.0, "city": ` + string(city) +
		`, "country_name": "` + long + `"}`

	fix, err := parseIPLookup([]byte(body))
	if err != nil {
		t.Fatalf("parseIPLookup: %v", err)
	}
	for _, r := range fix.Label {
		if r < 0x20 || r == 0x7f {
			t.Errorf("label kept control character %U: %q", r, fix.Label)
		}
	}
	const wantPrefix = "New York[31m, "
	if !strings.HasPrefix(fix.Label, wantPrefix) {
		t.Errorf("label is %q, want it to start with %q", fix.Label, wantPrefix)
	}
	if runes := len([]rune(fix.Label)); runes > len(wantPrefix)+maxPlaceNameRunes {
		t.Errorf("label is %d runes, past the cap: %q", runes, fix.Label)
	}
}
