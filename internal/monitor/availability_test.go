package monitor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Whether a sensor can work on this machine is a question that can take twenty
// seconds to answer: on Windows the lid asks WMI, and asking means starting
// PowerShell. The rules in availability.go are about who pays that, and getting
// them wrong does not break a sensor — it hangs a phone that is waiting to be
// let in, which looks like the product being broken from the only place anybody
// is looking.

// slowSensor takes as long to answer as it is told to, and counts how many
// times it was asked.
type slowSensor struct {
	name      string
	available bool
	takes     time.Duration
	asked     atomic.Int32
}

func newSlow(name string, available bool, takes time.Duration) *slowSensor {
	return &slowSensor{name: name, available: available, takes: takes}
}

func (s *slowSensor) Name() string        { return s.name }
func (s *slowSensor) DisplayName() string { return s.name }

func (s *slowSensor) Available() bool {
	s.asked.Add(1)
	time.Sleep(s.takes)
	return s.available
}

func (s *slowSensor) Start(ctx context.Context, _ chan<- Alert) error {
	<-ctx.Done()
	return nil
}

func (s *slowSensor) Stop() error { return nil }

func TestNobodyWaitsOnASensorThatHasNotAnsweredYet(t *testing.T) {
	// This is the whole point of the file. The pairing reply calls this, and a
	// phone sitting on a WMI query gives up after ten seconds.
	mgr := NewManager()
	lid := newSlow("lid", true, time.Hour)

	start := time.Now()
	available, known := mgr.AvailableNow(lid)
	took := time.Since(start)

	if took > time.Second {
		t.Errorf("the caller waited %s on a sensor that had not answered", took)
	}
	if known {
		t.Error("an answer nobody has worked out yet was reported as known")
	}
	if available {
		t.Error("a sensor that has not answered read as available; unavailable is the safe direction for an alarm")
	}
}

func TestTheAnswerIsWorkedOutInTheBackgroundAndThenRemembered(t *testing.T) {
	mgr := NewManager()
	power := newSlow("power", true, 0)

	mgr.AvailableNow(power) // starts the work

	waitFor(t, "the answer to settle", func() bool {
		available, known := mgr.AvailableNow(power)
		return known && available
	})
}

func TestASensorIsAskedOnceHoweverManyTimesItIsRead(t *testing.T) {
	// Every status broadcast calls this, which is every fifteen seconds for as
	// long as the program runs. Without the cache each one started PowerShell
	// to ask the same settled question again.
	mgr := NewManager()
	lid := newSlow("lid", true, 20*time.Millisecond)

	for range 50 {
		mgr.AvailableNow(lid)
	}
	waitFor(t, "the answer to settle", func() bool {
		_, known := mgr.AvailableNow(lid)
		return known
	})
	for range 50 {
		mgr.AvailableNow(lid)
	}

	if asked := lid.asked.Load(); asked != 1 {
		t.Errorf("the sensor was asked %d times; the answer cannot change while the program runs", asked)
	}
}

func TestASensorThatCannotWorkHereIsRememberedAsSuch(t *testing.T) {
	// A desktop has no lid. Reporting it as available would put a sensor on the
	// panel that can never fire.
	mgr := NewManager()
	lid := newSlow("lid", false, 0)

	mgr.AvailableNow(lid)

	waitFor(t, "the sensor to settle as unavailable", func() bool {
		available, known := mgr.AvailableNow(lid)
		return known && !available
	})
}

func TestPrimingAsksEveryoneAtOnceRatherThanOneAfterAnother(t *testing.T) {
	// Six sensors each allowed twenty seconds, one after another, is two
	// minutes of a laptop not knowing what it can watch.
	const each = 200 * time.Millisecond

	mgr := NewManager()
	for _, name := range []string{"power", "lid", "usb", "screen", "network", "input"} {
		mgr.Register(newSlow(name, true, each))
	}

	start := time.Now()
	mgr.PrimeAvailability()
	took := time.Since(start)

	if took > 3*each {
		t.Errorf("priming six sensors took %s, which is one after another rather than side by side", took)
	}
	for _, s := range mgr.Sensors() {
		if _, known := mgr.AvailableNow(s); !known {
			t.Errorf("%s had not answered by the time priming returned", s.Name())
		}
	}
}

func TestPrimingLeavesNothingForTheNextCallerToWaitOn(t *testing.T) {
	mgr := NewManager()
	lid := newSlow("lid", true, 10*time.Millisecond)
	mgr.Register(lid)

	mgr.PrimeAvailability()

	available, known := mgr.AvailableNow(lid)
	if !known || !available {
		t.Error("a primed sensor still had to be asked")
	}
	if asked := lid.asked.Load(); asked != 1 {
		t.Errorf("the sensor was asked %d times, so priming did not settle it", asked)
	}
}

func TestArmingKeepsTheAnswersItPaidFor(t *testing.T) {
	// Arming is the one caller allowed to wait for an availability answer. The
	// answers it gets are the same ones the panel reads, so throwing them away
	// would mean paying for them twice.
	mgr := NewManager()
	lid := newSlow("lid", true, 0)
	mgr.Register(lid)
	mgr.Enable("lid")

	mgr.StartEnabled()
	t.Cleanup(mgr.StopAll)

	available, known := mgr.AvailableNow(lid)
	if !known {
		t.Error("arming asked whether the sensor was available and then forgot")
	}
	if !available {
		t.Error("the answer arming paid for came back wrong")
	}
	if asked := lid.asked.Load(); asked != 1 {
		t.Errorf("the sensor was asked %d times for one arm", asked)
	}
}

// Arming waits for the answers it needs, and that is deliberate. What it must
// not do is wait for the sum of them: asked one after another, six sensors each
// allowed twenty seconds is two minutes — and on the terminal that is two
// minutes with nobody reading the keyboard, which is a dashboard the user
// cannot type at and cannot Ctrl+C out of.
func TestArmingAsksTheSensorsItDoesNotKnowAllAtOnce(t *testing.T) {
	const each = 200 * time.Millisecond

	mgr := NewManager()
	for _, name := range []string{"power", "lid", "usb", "screen", "network", "input"} {
		mgr.Register(newSlow(name, true, each))
		mgr.Enable(name)
	}

	start := time.Now()
	mgr.StartEnabled()
	took := time.Since(start)
	t.Cleanup(mgr.StopAll)

	if took > 3*each {
		t.Errorf("arming took %s to ask six sensors, which is one after another rather than side by side", took)
	}
}

// And having been told once, it does not ask again. The answer is settled for
// the run — that is what the cache is for — so a second arm costs nothing, and
// on Windows "nothing" is the difference between arming and starting PowerShell
// six more times.
func TestArmingDoesNotAskAgainWhatIsAlreadySettled(t *testing.T) {
	mgr := NewManager()
	lid := newSlow("lid", true, 0)
	mgr.Register(lid)
	mgr.Enable("lid")

	mgr.PrimeAvailability()
	mgr.StartEnabled()
	mgr.StopAll()
	mgr.StartEnabled()
	t.Cleanup(mgr.StopAll)

	if asked := lid.asked.Load(); asked != 1 {
		t.Errorf("the sensor was asked %d times across a prime and two arms, want once", asked)
	}
}
