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
