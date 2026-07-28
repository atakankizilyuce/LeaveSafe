package ws

import (
	"time"

	"github.com/leavesafe/leavesafe/internal/location"
)

// locationPayload converts a tracker snapshot into the wire form.
func locationPayload(enabled bool, snap location.Snapshot) LocationPayload {
	return LocationPayload{
		Enabled:   enabled,
		Available: snap.Available,
		Fix:       fixToWire(snap.Fix),
		Anchor:    fixToWire(snap.Anchor),
		MovedM:    snap.MovedM,
		Moved:     snap.Moved,
	}
}

func fixToWire(fix *location.Fix) *LocationFix {
	if fix == nil {
		return nil
	}
	return &LocationFix{
		Latitude:  fix.Latitude,
		Longitude: fix.Longitude,
		AccuracyM: fix.AccuracyM,
		Source:    string(fix.Source),
		Timestamp: fix.Timestamp.Unix(),
		Label:     fix.Label,
	}
}

// anchorFromWire converts a position the phone reported into a fix.
//
// It rejects coordinates outside their valid ranges. The browser geolocation
// API will not produce those, but the hub cannot tell a browser from anything
// else that has the pairing key, and a bogus anchor would silently poison every
// movement calculation that follows.
func anchorFromWire(in *LocationFix) *location.Fix {
	if in == nil {
		return nil
	}
	if in.Latitude < -90 || in.Latitude > 90 {
		return nil
	}
	if in.Longitude < -180 || in.Longitude > 180 {
		return nil
	}

	accuracy := in.AccuracyM
	if accuracy <= 0 {
		// A phone that reports no accuracy is not claiming perfect accuracy.
		// Typical assisted GPS lands around 10 m, so that is the assumption
		// rather than zero, which would render as certainty.
		accuracy = 10
	}

	ts := time.Now()
	if in.Timestamp > 0 {
		ts = time.Unix(in.Timestamp, 0)
	}

	return &location.Fix{
		Latitude:  in.Latitude,
		Longitude: in.Longitude,
		AccuracyM: accuracy,
		Source:    location.SourcePhone,
		Timestamp: ts,
	}
}
