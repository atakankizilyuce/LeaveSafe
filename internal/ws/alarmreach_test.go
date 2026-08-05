package ws

import (
	"encoding/json"
	"sync"
	"testing"
)

// recorder is a transport that keeps what the hub wrote to it, so a test can
// ask what a phone would have been told.
type recorder struct {
	mu   sync.Mutex
	sent []ServerMessage
}

func (r *recorder) Send(data []byte) error {
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}
	r.mu.Lock()
	r.sent = append(r.sent, msg)
	r.mu.Unlock()
	return nil
}

func (r *recorder) Close() error { return nil }

// saw reports whether a message of this type was written, and returns it.
func (r *recorder) saw(msgType string) (ServerMessage, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.sent {
		if m.Type == msgType {
			return m, true
		}
	}
	return ServerMessage{}, false
}

// pair registers a client on this hub and takes it through the pairing key, the
// way a phone's first two messages do.
func (h *Hub) pairRecorder(t *testing.T) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{}
	client := h.RegisterExternalClient(rec, nil)
	h.handleMessage(client, ClientMessage{Type: MsgTypeAuth, Key: h.authManager.RawPairingKey()})
	if !client.authenticated {
		t.Fatal("the stand-in phone did not pair")
	}
	return client, rec
}

// A phone drops its socket every time its screen locks, so the phone that
// reconnects into a sounding alarm is the ordinary case. It used to arrive at a
// calm panel: no overlay, no siren, nothing to dismiss — while the laptop
// screamed on the table and the hub, still holding the alarm as active,
// swallowed every event that came after it.
func TestAPhoneThatPairsDuringAnAlarmIsToldAboutIt(t *testing.T) {
	hub := triggerHub(t)
	hub.Arm()
	hub.TriggerSensorTest("power")

	_, rec := hub.pairRecorder(t)

	msg, ok := rec.saw(MsgTypeAlarmActive)
	if !ok {
		t.Fatal("a phone pairing into a sounding alarm was told nothing about it")
	}
	if msg.Alert == nil || msg.Alert.Sensor != "power" {
		t.Errorf("the alarm did not name the sensor that raised it: %+v", msg.Alert)
	}
	if msg.Alert != nil && msg.Alert.Message == "" {
		t.Error("the alarm carried no message for the overlay to show")
	}
}

// Pairing while nothing is sounding must stay quiet, or every reconnect would
// raise an alarm of its own.
func TestAPhoneThatPairsWithNoAlarmHearsNone(t *testing.T) {
	hub := triggerHub(t)
	hub.Arm()

	_, rec := hub.pairRecorder(t)

	if _, ok := rec.saw(MsgTypeAlarmActive); ok {
		t.Error("pairing raised an alarm that was not sounding")
	}
}

// An alarm is one event on several devices, and only the device that answered
// it knows it has been called off. Dismissing from one phone left every other
// phone sounding, its overlay offering to pause a sensor that was no longer
// alarming.
func TestDismissingFromOnePhoneReachesTheOthers(t *testing.T) {
	hub := triggerHub(t)
	hub.Arm()
	hub.TriggerSensorTest("power")

	first, _ := hub.pairRecorder(t)
	_, second := hub.pairRecorder(t)

	hub.handleMessage(first, ClientMessage{Type: MsgTypeDismissAlarm})

	if _, ok := second.saw(MsgTypeAlarmCleared); !ok {
		t.Error("the other phone was never told the alarm had been answered")
	}
}

// Disarming stops the alarm on the laptop, and used to leave every phone
// sounding at one the machine had already stopped having.
func TestDisarmingTellsThePhonesTheAlarmIsOver(t *testing.T) {
	hub := triggerHub(t)
	hub.Arm()
	hub.TriggerSensorTest("power")

	_, rec := hub.pairRecorder(t)
	hub.Disarm()

	if _, ok := rec.saw(MsgTypeAlarmCleared); !ok {
		t.Error("disarming silenced the laptop and left the phone sounding")
	}
}

// The state the alarm leaves behind is what suppresses the next one, so a
// dismissal has to clear it or the second alarm never sounds anywhere.
func TestASecondAlarmSoundsAfterTheFirstIsDismissed(t *testing.T) {
	hub := triggerHub(t)
	hub.Arm()

	var triggers int
	hub.SetAlarmTriggerCallback(func() { triggers++ })

	hub.TriggerSensorTest("power")
	client, _ := hub.pairRecorder(t)
	hub.handleMessage(client, ClientMessage{Type: MsgTypeDismissAlarm})
	hub.TriggerSensorTest("power")

	if triggers != 2 {
		t.Errorf("the laptop's siren was started %d times, want 2 — the second alarm was swallowed", triggers)
	}
	if _, _, active := hub.activeAlarm(); !active {
		t.Error("the second alarm was not recorded, so nothing could answer it")
	}
}
