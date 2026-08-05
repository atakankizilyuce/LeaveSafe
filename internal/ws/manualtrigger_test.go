package ws

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
)

// quietSensor is registered so the hub knows the name, and never fires. The
// point of these tests is the manual trigger, which needs no sensor event.
//
// turns counts how often the manager started it. Enabling a sensor and starting
// it are separate steps, and a test that only asks whether it is enabled cannot
// tell the two apart.
type quietSensor struct {
	name  string
	turns atomic.Int32
}

func (s *quietSensor) Name() string        { return s.name }
func (s *quietSensor) DisplayName() string { return s.name }
func (s *quietSensor) Available() bool     { return true }
func (s *quietSensor) Stop() error         { return nil }

func (s *quietSensor) Start(ctx context.Context, _ chan<- monitor.Alert) error {
	s.turns.Add(1)
	<-ctx.Done()
	return nil
}

func triggerHub(t *testing.T) *Hub {
	t.Helper()
	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	mgr := monitor.NewManager()
	mgr.Register(&quietSensor{name: "power"})
	mgr.Enable("power")
	return NewHub(authMgr, mgr, "test")
}

// The phone's alert overlay offers three answers, and two of them — pause this
// sensor, stop using this sensor — act on the sensor the hub recorded as having
// raised the alarm, never on the name the message carried. A trigger fired by
// hand raised the alarm without recording anything, so both answers silently did
// nothing: the user tapped "pause this sensor", the sensor was not paused, and
// the only trace was a debug line on a laptop they were not standing next to.
func TestAManualTriggerRecordsTheAlarmItRaised(t *testing.T) {
	hub := triggerHub(t)
	hub.Arm()

	if !hub.TriggerSensorTest("power") {
		t.Fatal("the trigger did not find the sensor it was given")
	}

	if got := hub.clearAlarm(); got != "power" {
		t.Errorf("the alarm names %q, so an answer to it would act on nothing; want %q", got, "power")
	}
}

// Pausing the sensor is the answer that has to reach the sensor manager, and it
// is the one that reads the recorded name to know which one to pause.
func TestPausingASensorAnswersAManualTrigger(t *testing.T) {
	hub := triggerHub(t)
	hub.Arm()
	hub.TriggerSensorTest("power")

	token, _, err := hub.authManager.Authenticate(auth.UnknownAddr, hub.authManager.RawPairingKey())
	if err != nil {
		t.Fatalf("pair the stand-in phone: %v", err)
	}
	client := hub.RegisterExternalClient(nopTransport{}, nil)
	client.authenticated = true
	client.token = token
	hub.handleMessage(client, ClientMessage{Type: MsgTypeDismissAlarmPause, Duration: 60})

	if hub.sensorMgr.IsEnabled("power") {
		t.Error("the sensor is still on, so the pause answered no alarm")
	}
}

// A trigger fired while nothing is armed is a self-test, not an alarm. It must
// not leave alarm state behind: the next real event would then be discarded as a
// re-trigger of an alarm that never happened.
func TestAManualTriggerOnADisarmedHubRecordsNoAlarm(t *testing.T) {
	hub := triggerHub(t)

	if !hub.TriggerSensorTest("power") {
		t.Fatal("the trigger did not find the sensor it was given")
	}

	hub.mu.RLock()
	active := hub.alarmActive
	hub.mu.RUnlock()
	if active {
		t.Error("a disarmed hub recorded an active alarm, which would swallow the next real one")
	}
}

// nopTransport stands in for a socket so a client can be handed to handleMessage
// without one. Nothing here reads what the hub writes back.
type nopTransport struct{}

func (nopTransport) Send([]byte) error { return nil }
func (nopTransport) Close() error      { return nil }
