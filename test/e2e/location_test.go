//go:build e2e

package e2e_test

import (
	"math"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

// These tests seed only the phone anchor as a position source, so nothing here
// scans for Wi-Fi or calls a geolocation service. The suite stays hermetic and
// does not go red because a third-party API had a bad afternoon.

// TestLocation_DisabledByDefault is the privacy guarantee. Location must be
// something a user switches on, never something they discover was already on.
func TestLocation_DisabledByDefault(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())
	phone.Authenticate(app.Key())

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetLocation})
	reply := phone.Expect(ws.MsgTypeLocation, 5*time.Second)

	if reply.Location == nil {
		t.Fatal("location reply carried no payload")
	}
	if reply.Location.Enabled {
		t.Error("location tracking is enabled without the user asking for it")
	}
}

// TestLocation_AnchorRoundTrip covers the core flow: the phone reports its own
// position while standing next to the laptop, and the laptop reports it back as
// where it is.
func TestLocation_AnchorRoundTrip(t *testing.T) {
	app := harness.Start(t, harness.Options{LocationEnabled: true})
	phone := harness.Dial(t, app.Port())
	phone.Authenticate(app.Key())

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})
	phone.Send(ws.ClientMessage{
		Type: ws.MsgTypeLocationAnchor,
		Location: &ws.LocationFix{
			Latitude:  41.0082,
			Longitude: 28.9784,
			AccuracyM: 8,
		},
	})

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetLocation})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		reply := phone.Expect(ws.MsgTypeLocation, 5*time.Second)
		if reply.Location == nil || reply.Location.Fix == nil {
			phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetLocation})
			continue
		}

		fix := reply.Location.Fix
		if fix.Source != "phone" {
			t.Errorf("source = %q, want %q", fix.Source, "phone")
		}
		if math.Abs(fix.Latitude-41.0082) > 0.0001 || math.Abs(fix.Longitude-28.9784) > 0.0001 {
			t.Errorf("position = %f,%f want 41.0082,28.9784", fix.Latitude, fix.Longitude)
		}
		if fix.AccuracyM != 8 {
			t.Errorf("accuracy = %f, want 8", fix.AccuracyM)
		}
		if reply.Location.Moved {
			t.Error("moved = true with only one position reported")
		}
		return
	}
	t.Fatal("the anchor never came back as a position")
}

// TestLocation_RejectsImpossibleCoordinates guards the movement calculation.
// The hub cannot tell a browser from anything else holding the pairing key, and
// a nonsense anchor would silently poison every distance derived from it.
func TestLocation_RejectsImpossibleCoordinates(t *testing.T) {
	app := harness.Start(t, harness.Options{LocationEnabled: true})
	phone := harness.Dial(t, app.Port())
	phone.Authenticate(app.Key())

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})
	phone.Send(ws.ClientMessage{
		Type:     ws.MsgTypeLocationAnchor,
		Location: &ws.LocationFix{Latitude: 999, Longitude: -4000, AccuracyM: 5},
	})
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetLocation})

	reply := phone.Expect(ws.MsgTypeLocation, 5*time.Second)
	if reply.Location == nil {
		t.Fatal("location reply carried no payload")
	}
	if reply.Location.Fix != nil {
		t.Errorf("an out-of-range anchor was accepted: %+v", reply.Location.Fix)
	}
}

// TestLocation_APIKeyIsNeverSentToTheClient pins the same treatment the PIN
// gets. A paired phone is trusted to arm the alarm, not to be handed a billable
// credential.
func TestLocation_APIKeyIsNeverSentToTheClient(t *testing.T) {
	const secret = "AIzaTHIS-MUST-NOT-LEAVE-THE-LAPTOP"

	app := harness.Start(t, harness.Options{
		LocationEnabled:      true,
		LocationGeolocateKey: secret,
	})
	phone := harness.Dial(t, app.Port())
	phone.Authenticate(app.Key())

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	reply := phone.Expect(ws.MsgTypeConfigData, 5*time.Second)

	if reply.Config == nil {
		t.Fatal("config_data carried no config")
	}
	if reply.Config.Location.GeolocateKey != "" {
		t.Errorf("the geolocation API key was sent to the client: %q", reply.Config.Location.GeolocateKey)
	}
	if !reply.Config.Location.HasKey {
		t.Error("has_geolocate_key = false, so the settings screen cannot tell the user a key is configured")
	}
}

// TestLocation_SurvivesDisarm keeps the last known position on screen instead
// of blanking the panel the moment the user disarms.
func TestLocation_SurvivesDisarm(t *testing.T) {
	app := harness.Start(t, harness.Options{LocationEnabled: true})
	phone := harness.Dial(t, app.Port())
	phone.Authenticate(app.Key())

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})
	phone.Send(ws.ClientMessage{
		Type:     ws.MsgTypeLocationAnchor,
		Location: &ws.LocationFix{Latitude: 41.0082, Longitude: 28.9784, AccuracyM: 8},
	})
	phone.Expect(ws.MsgTypeLocation, 5*time.Second)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeDisarm})
	time.Sleep(500 * time.Millisecond)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetLocation})
	reply := phone.Expect(ws.MsgTypeLocation, 5*time.Second)

	if reply.Location == nil || reply.Location.Fix == nil {
		t.Fatal("disarming discarded the last known position")
	}
}
