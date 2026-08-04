// Package location determines where the monitored machine is, so the phone can
// see it while the system is armed.
//
// A laptop has no GPS receiver, so there is no single source of truth here.
// Three sources are combined, none of which is sufficient alone:
//
//   - the paired phone's GPS, captured when the system is armed. The phone and
//     the laptop are in the same place at that moment, so this is the most
//     precise fix available — right up until the laptop is carried away, at
//     which point it is precisely wrong.
//   - a Wi-Fi scan resolved through a geolocation service. Accurate to tens of
//     meters and genuinely live, but needs an API key and internet access.
//   - the public IP address. Always available with internet, and accurate to
//     somewhere between a neighborhood and a province.
//
// Every fix therefore carries the source that produced it and an accuracy
// radius, and callers are expected to render both. Reporting a 20 km guess as a
// pin on a map would be a lie the user cannot detect.
package location

import (
	"context"
	"math"
	"time"
)

// Source identifies what produced a fix.
type Source string

const (
	// SourcePhone is the paired phone's GPS, reported when the system is armed.
	SourcePhone Source = "phone"
	// SourceWiFi is a Wi-Fi scan resolved by a geolocation service.
	SourceWiFi Source = "wifi"
	// SourceIP is a lookup of the public IP address.
	SourceIP Source = "ip"
)

// Fix is a single position estimate.
type Fix struct {
	Latitude  float64   `json:"lat"`
	Longitude float64   `json:"lon"`
	AccuracyM float64   `json:"accuracy_m"`
	Source    Source    `json:"source"`
	Timestamp time.Time `json:"ts"`
	// Label is a human-readable place name when the source produced one, such
	// as the city an IP lookup resolved to. Empty otherwise.
	Label string `json:"label,omitempty"`
}

// ValidCoordinates reports whether lat and lon are an ordinary point on the
// globe rather than a number that only survives because nothing looked.
//
// Every fix in this package comes from outside it — a third-party service over
// the network, or the paired phone — and none of those is a source this program
// gets to assume good behavior from. A pair of coordinates that is out of range
// or not a number reaches the tracker, spreads into the distance-moved figure
// the phone reads as "moved 40 m from where you armed it", and settles there:
// the panel that exists to say where the machine is would be stating something
// nobody measured. The infinities are named separately because a range check
// alone lets them through — every comparison against NaN is false, and one that
// gets this far then fails to encode as JSON, which takes the position panel
// quiet for the rest of the session.
func ValidCoordinates(lat, lon float64) bool {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return false
	}
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// Provider produces fixes from one source.
type Provider interface {
	// Source reports which source this provider represents.
	Source() Source
	// Available reports whether this provider can run at all on this machine
	// and with this configuration. An unavailable provider is never polled.
	Available() bool
	// Locate produces a fix, or an error explaining why it could not.
	Locate(ctx context.Context) (*Fix, error)
}
