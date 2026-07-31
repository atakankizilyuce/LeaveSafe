package ws

import (
	"testing"
	"time"
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
