package ws

import (
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/config"
)

// Every message from a paired phone arrives through one door, and the door does
// three things before it lets anything past: it proves the session is still
// alive, it meters how fast the phone may send, and only then does it act. Each
// of those is a place the message can end, and each of them is why the rest of
// the hub can assume what it assumes.

func TestArmingAndDisarmingFromThePhone(t *testing.T) {
	hub := triggerHub(t)
	client, _ := hub.pairRecorder(t)

	hub.handleMessage(client, ClientMessage{Type: MsgTypeArm})
	if !hub.IsArmed() {
		t.Fatal("the arm message did not arm the machine")
	}

	hub.handleMessage(client, ClientMessage{Type: MsgTypeDisarm})
	if hub.IsArmed() {
		t.Error("the disarm message did not disarm the machine")
	}
}

// With a PIN set, disarming is a question rather than an instruction. Acting on
// it and asking afterwards would make the PIN decorative.
func TestDisarmingWithAPINSetAsksForItInsteadOfDisarming(t *testing.T) {
	hub := triggerHub(t)
	client, rec := hub.pairRecorder(t)
	hub.SetPinProtection(true, "a-hash-that-nothing-matches")
	hub.Arm()

	hub.handleMessage(client, ClientMessage{Type: MsgTypeDisarm})

	if _, ok := rec.saw(MsgTypePinRequired); !ok {
		t.Error("the phone was not asked for the PIN")
	}
	if !hub.IsArmed() {
		t.Error("the machine disarmed without the PIN it had asked for")
	}
}

func TestAWrongPINIsRefusedRatherThanDisarming(t *testing.T) {
	hub := triggerHub(t)
	client, rec := hub.pairRecorder(t)
	hub.SetPinProtection(true, "a-hash-that-nothing-matches")
	hub.Arm()

	hub.handleMessage(client, ClientMessage{Type: MsgTypeDisarmPin, Pin: "0000"})

	if _, ok := rec.saw(MsgTypeAuthFail); !ok {
		t.Error("a wrong PIN was not refused")
	}
	if !hub.IsArmed() {
		t.Error("a wrong PIN disarmed the machine")
	}
}

func TestTheSelfTestAlertReachesThePhone(t *testing.T) {
	hub := triggerHub(t)
	client, rec := hub.pairRecorder(t)

	hub.handleMessage(client, ClientMessage{Type: MsgTypeTestAlert})

	msg, ok := rec.saw(MsgTypeAlert)
	if !ok || msg.Alert == nil {
		t.Fatal("the test alert never reached the phone")
	}
	if msg.Alert.Sensor != "test" {
		t.Errorf("the test alert claimed to come from %q", msg.Alert.Sensor)
	}
}

// A sensor test with no sensor named is not a request the hub can answer, and
// answering it with whichever sensor happened to be first would be worse than
// ignoring it.
func TestASensorTestWithNoSensorNamedIsIgnored(t *testing.T) {
	hub := triggerHub(t)
	client, _ := hub.pairRecorder(t)
	hub.Arm()

	hub.handleMessage(client, ClientMessage{Type: MsgTypeTriggerSensor})

	if _, active := alarmState(hub); active {
		t.Error("a sensor test with no sensor named raised an alarm anyway")
	}

	hub.handleMessage(client, ClientMessage{Type: MsgTypeTriggerSensor, Sensor: "power"})
	sensor, active := alarmState(hub)
	if !active || sensor != "power" {
		t.Errorf("the named sensor was not tested; alarm is %q active=%v", sensor, active)
	}
}

func TestThePhoneCanAskForTheSettingsAndThePosition(t *testing.T) {
	hub := triggerHub(t)
	hub.SetConfig(config.Default())
	client, rec := hub.pairRecorder(t)

	hub.handleMessage(client, ClientMessage{Type: MsgTypeGetConfig})
	if _, ok := rec.saw(MsgTypeConfigData); !ok {
		t.Error("the settings were never sent")
	}

	hub.handleMessage(client, ClientMessage{Type: MsgTypeGetLocation})
	if _, ok := rec.saw(MsgTypeLocation); !ok {
		t.Error("the position was never sent")
	}
}

// The phone offers its own position as the anchor whenever it arms, and it does
// so without knowing whether the laptop is tracking location at all. With no
// tracker there is nothing to hold the anchor, and the message has to be
// dropped rather than reaching for one.
func TestAnAnchorOfferedWithNoTrackerIsDropped(t *testing.T) {
	hub := triggerHub(t)
	cfg := config.Default()
	cfg.Location.PhoneAnchor = true
	hub.SetConfig(cfg)
	client, _ := hub.pairRecorder(t)

	hub.handleMessage(client, ClientMessage{
		Type:     MsgTypeLocationAnchor,
		Location: &LocationFix{Latitude: 51.5, Longitude: -0.12, AccuracyM: 8},
	})

	if payload := hub.LocationPayload(); payload.Anchor != nil {
		t.Errorf("an anchor was recorded with nothing to track it; payload was %+v", payload)
	}
}

// Resetting is the one settings change that has to reach the sensor manager
// without being told which sensors to touch: the defaults name none, and that
// means all of them.
func TestResettingTheSettingsFromThePhone(t *testing.T) {
	hub := triggerHub(t)
	cfg := config.Default()
	cfg.Port = cfg.Port + 1
	hub.SetConfig(cfg)
	client, _ := hub.pairRecorder(t)

	hub.handleMessage(client, ClientMessage{Type: MsgTypeResetConfig})

	hub.mu.RLock()
	port := hub.cfg.Port
	hub.mu.RUnlock()
	if port != config.Default().Port {
		t.Errorf("the reset left the port at %d", port)
	}
}

// Dismissing the alarm from the phone means picking the laptop up, and to the
// input sensor that is itself input. Without the moment of quiet, the tap that
// cleared the alarm raises the next one.
func TestDismissingAnInputAlarmHoldsThatSensorQuietForAMoment(t *testing.T) {
	hub, _ := hubWithSensors(t, "input")
	listeningClient(t, hub)
	hub.Arm()
	hub.dispatchAlert(alertFrom("input", "the laptop was moved"))

	hub.dismissAlarm()

	hub.mu.RLock()
	until, suppressed := hub.suppressedSensors["input"]
	hub.mu.RUnlock()
	if !suppressed {
		t.Fatal("the input sensor was not held quiet after its alarm was dismissed")
	}
	if !until.After(time.Now()) {
		t.Error("the quiet period was already over when it was set")
	}
	if _, active := alarmState(hub); active {
		t.Error("the alarm was still sounding after being dismissed")
	}
}

// Any other sensor gets no grace period: the charger being pulled is not
// something dismissing the alarm can do by accident.
func TestDismissingAnyOtherAlarmSuppressesNothing(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	listeningClient(t, hub)
	hub.Arm()
	hub.dispatchAlert(alertFrom("power", "charger disconnected"))

	hub.dismissAlarm()

	hub.mu.RLock()
	count := len(hub.suppressedSensors)
	hub.mu.RUnlock()
	if count != 0 {
		t.Errorf("dismissing a charger alarm suppressed %d sensor(s)", count)
	}
}

// A paused sensor has to come back on its own, and on a machine still being
// watched it has to start watching again — otherwise "pause for five seconds"
// is "switch off until the next arm".
func TestAPausedSensorComesBackAndWatchesAgain(t *testing.T) {
	hub, sensors := hubWithSensors(t, "power")
	listeningClient(t, hub)
	hub.sensorMgr.Enable("power")
	hub.Arm()
	hub.sensorMgr.Disable("power")

	hub.unpauseSensor("power", 0)

	if !hub.sensorMgr.IsEnabled("power") {
		t.Fatal("the paused sensor was never switched back on")
	}
	waitUntil(t, "the sensor came back but was never started again", func() bool {
		return sensors[0].turns.Load() > 0
	})
}

// The idle timeout is measured in messages, and a session that has run out has
// to be refused rather than served. Serving it would mean the timeout only ever
// applied to a phone that had stopped talking anyway.
func TestAMessageOnAnExpiredSessionIsRefusedAndTheClientDropped(t *testing.T) {
	hub := triggerHub(t)
	client, rec := hub.pairRecorder(t)
	client.token = "a-token-this-hub-never-issued"

	hub.handleMessage(client, ClientMessage{Type: MsgTypeArm})

	if hub.IsArmed() {
		t.Error("a message on an expired session was acted on")
	}
	msg, ok := rec.saw(MsgTypeAuthFail)
	if !ok {
		t.Fatal("the phone was not told its session had expired")
	}
	if msg.Reason == "" {
		t.Error("the refusal gave no reason for the phone to show")
	}
	if hub.ClientCount() != 0 {
		t.Error("the client whose session expired was left connected")
	}
}
