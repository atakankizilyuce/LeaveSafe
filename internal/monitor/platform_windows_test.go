//go:build windows

package monitor

import (
	"context"
	"testing"
	"time"
)

// The one part of a sensor a fake cannot stand in for: the call that asks
// Windows. What is worth pinning here is not the answer — it depends on the
// machine the test is running on — but that asking works at all, and that a
// question this product cannot afford to wait on can be called off.

func TestTheChargerCanBeReadOnThisMachine(t *testing.T) {
	// A Win32 call, so this is quick and it either works or the API is not
	// there. What it catches is the struct layout: GetSystemPowerStatus writes
	// into memory this package describes by hand, and a field in the wrong
	// place reads the battery percentage as the charger.
	status, err := getPowerStatus()
	if err != nil {
		t.Skipf("this machine has no power status to read: %v", err)
	}
	if status.ACLineStatus != acOffline && status.ACLineStatus != acOnline && status.ACLineStatus != 255 {
		t.Errorf("ACLineStatus was %d, which is not one of the three documented answers", status.ACLineStatus)
	}

	onAC, err := readOnAC()
	if err == nil && status.ACLineStatus == 255 {
		t.Error("an unknown reading came back as an answer")
	}
	if err == nil {
		t.Logf("the charger is in: %v", onAC)
	}
}

func TestTheIdleClockCanBeReadOnThisMachine(t *testing.T) {
	// GetLastInputInfo needs its own size written into the struct before it is
	// called. Get that wrong and it fails silently, which the sensor reads as a
	// machine nobody is touching — an alarm that never fires.
	first := getLastInput()
	if first == 0 {
		t.Skip("this machine does not report an idle clock")
	}

	time.Sleep(20 * time.Millisecond)
	if again := getLastInput(); again < first {
		t.Errorf("the idle clock went backwards: %d then %d", first, again)
	}
}

func TestAQueryIsCalledOffWhenTheSensorIsDisarmed(t *testing.T) {
	// The lid and the screen are read by starting PowerShell, which takes long
	// enough that disarming has to interrupt it rather than wait for it. The
	// context these are handed is the sensor's own, so a disarm reaches them.
	stopped, cancel := context.WithCancel(context.Background())
	cancel()

	for name, ask := range map[string]func(context.Context) (bool, error){
		"lid":    isLidOpenWindows,
		"screen": isScreenOnWindows,
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			_, err := ask(stopped)
			took := time.Since(start)

			if err == nil {
				t.Error("a query run under a cancelled context came back with an answer")
			}
			if took > 5*time.Second {
				t.Errorf("a disarmed sensor sat on its query for %s", took)
			}
		})
	}
}
