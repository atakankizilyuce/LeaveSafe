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
