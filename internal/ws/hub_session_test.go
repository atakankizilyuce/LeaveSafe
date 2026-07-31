package ws

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"nhooyr.io/websocket"
)

// The phone shows how long the machine has been armed. That counter has to come
// from the laptop, because the page holding it is discarded every time the phone
// locks its screen.
func TestArmRecordsWhenItHappened(t *testing.T) {
	hub := testHub(t)

	if got := hub.ArmedAt(); !got.IsZero() {
		t.Errorf("a disarmed hub reported an arm time of %v", got)
	}

	before := time.Now()
	hub.Arm()
	got := hub.ArmedAt()

	if got.IsZero() {
		t.Fatal("arming recorded no time")
	}
	if got.Before(before.Add(-time.Second)) || got.After(time.Now().Add(time.Second)) {
		t.Errorf("arm time %v is not around now (%v)", got, before)
	}

	hub.Disarm()
	if got := hub.ArmedAt(); !got.IsZero() {
		t.Errorf("disarming left an arm time of %v", got)
	}
}

// A restart that re-arms did not start a new watch — it resumed the one the
// previous run was keeping, and the counter has to say so.
func TestRestoreArmedKeepsTheOriginalTime(t *testing.T) {
	hub := testHub(t)
	original := time.Now().Add(-2 * time.Hour)

	hub.RestoreArmed(original)

	if !hub.IsArmed() {
		t.Fatal("RestoreArmed did not arm the hub")
	}
	if got := hub.ArmedAt(); !got.Equal(original) {
		t.Errorf("arm time is %v, want the original %v", got, original)
	}
}

// Nothing recorded the moment on an older state file, and inventing one two
// hours in the past would be worse than admitting it started now.
func TestRestoreArmedWithoutATimeUsesNow(t *testing.T) {
	hub := testHub(t)

	hub.RestoreArmed(time.Time{})

	if got := hub.ArmedAt(); got.IsZero() {
		t.Error("RestoreArmed with an unknown time left no arm time at all")
	}
}

// A phone that locked its screen and came back has to learn the state on
// arrival. Waiting for the next heartbeat means up to fifteen seconds showing
// "disarmed" for a machine that is armed — the one thing the panel must never
// get wrong.
func TestAuthOKCarriesTheArmedState(t *testing.T) {
	hub := testHub(t)
	hub.Arm()

	msg := NewAuthOK("tok", nil, "test", hub.IsArmed(), hub.ArmedAt())

	if msg.Armed == nil || !*msg.Armed {
		t.Fatal("auth_ok did not say the machine was armed")
	}
	if msg.ArmedSince == nil {
		t.Fatal("auth_ok carried no arm time")
	}
	if want := hub.ArmedAt().Unix(); *msg.ArmedSince != want {
		t.Errorf("auth_ok said armed since %d, want %d", *msg.ArmedSince, want)
	}
}

// Claiming an arm time for a disarmed machine would give the phone a counter to
// render for a watch that is not running.
func TestAuthOKOmitsTheArmTimeWhenDisarmed(t *testing.T) {
	hub := testHub(t)

	msg := NewAuthOK("tok", nil, "test", hub.IsArmed(), hub.ArmedAt())

	if msg.Armed == nil || *msg.Armed {
		t.Error("auth_ok claimed a disarmed machine was armed")
	}
	if msg.ArmedSince != nil {
		t.Errorf("auth_ok carried an arm time of %d while disarmed", *msg.ArmedSince)
	}
}

// Silencing from the terminal has to silence the phone too. Stopping only the
// laptop leaves the phone wailing in a pocket with no way to know it is over.
func TestDismissAlarmTellsEveryPhone(t *testing.T) {
	hub := testHub(t)

	var dismissed atomic.Bool
	hub.SetAlarmDismissCallback(func() { dismissed.Store(true) })

	srv := hubServer(t, hub)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAndAuth(t, ctx, wsURL(srv), hub)
	defer conn.Close(websocket.StatusNormalClosure, "")

	hub.DismissAlarm()

	if !dismissed.Load() {
		t.Error("DismissAlarm did not stop the laptop's own alarm")
	}

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			t.Fatal("no alarm_cleared reached the phone")
		}
		var msg ServerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Type == MsgTypeAlarmCleared {
			return
		}
	}
}

// The PIN guards the alarm from whoever is holding the device. A terminal is a
// device too, and a wrong PIN there must leave the machine armed.
func TestConsoleDisarmRefusesAWrongPin(t *testing.T) {
	hash, err := auth.HashPin("1234")
	if err != nil {
		t.Fatalf("hash pin: %v", err)
	}

	hub := testHub(t)
	hub.SetPinProtection(true, hash)
	hub.Arm()

	if err := hub.DisarmWithPin("console", "9999"); err == nil {
		t.Error("a wrong PIN was accepted")
	}
	if !hub.IsArmed() {
		t.Error("a refused PIN disarmed the machine anyway")
	}

	if err := hub.DisarmWithPin("console", "1234"); err != nil {
		t.Errorf("the correct PIN was refused: %v", err)
	}
	if hub.IsArmed() {
		t.Error("the correct PIN did not disarm the machine")
	}
}

// With no PIN configured, disarming must not invent a barrier.
func TestConsoleDisarmWithoutAPinJustDisarms(t *testing.T) {
	hub := testHub(t)
	hub.Arm()

	if err := hub.DisarmWithPin("console", ""); err != nil {
		t.Errorf("disarm was refused with no PIN protection on: %v", err)
	}
	if hub.IsArmed() {
		t.Error("the machine stayed armed")
	}
}
