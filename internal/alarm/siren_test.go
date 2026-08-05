package alarm

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/config"
)

// audioSpy stands in for the operating system's audio control. Every call is
// recorded so a test can assert on what the alarm asked for, and beeps return
// at once rather than sounding for 400ms each.
type audioSpy struct {
	mu sync.Mutex

	maxCalls     int
	setLevels    []float64
	restoredTo   []float64
	beepFreqs    []int
	beeped       chan struct{}
	currentLevel float64

	maxErr     error
	setErr     error
	restoreErr error
}

func newAudioSpy() *audioSpy {
	return &audioSpy{currentLevel: 0.3, beeped: make(chan struct{}, 64)}
}

// install points the package at this spy for the duration of one test.
func (s *audioSpy) install(t *testing.T) {
	t.Helper()

	realMax, realSet, realRestore, realBeep := maxVolumeFn, setVolumeFn, restoreVolumeFn, beepToneFn
	t.Cleanup(func() {
		maxVolumeFn, setVolumeFn, restoreVolumeFn, beepToneFn = realMax, realSet, realRestore, realBeep
	})

	maxVolumeFn = func() (float64, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.maxCalls++
		if s.maxErr != nil {
			return 0, s.maxErr
		}
		return s.currentLevel, nil
	}
	setVolumeFn = func(level float64) (float64, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.setLevels = append(s.setLevels, level)
		if s.setErr != nil {
			return 0, s.setErr
		}
		return s.currentLevel, nil
	}
	restoreVolumeFn = func(level float64) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.restoredTo = append(s.restoredTo, level)
		return s.restoreErr
	}
	beepToneFn = func(freq int, _ int, stop <-chan struct{}) {
		select {
		case <-stop:
			return
		default:
		}
		s.mu.Lock()
		if len(s.beepFreqs) < 64 {
			s.beepFreqs = append(s.beepFreqs, freq)
		}
		s.mu.Unlock()
		select {
		case s.beeped <- struct{}{}:
		default:
		}
		// The siren loop calls this back to back with nothing else in it. Without
		// a pause the goroutine spins a core flat for the length of the test.
		time.Sleep(time.Millisecond)
	}
}

// waitForBeep blocks until the siren has sounded at least once.
func (s *audioSpy) waitForBeep(t *testing.T) {
	t.Helper()
	select {
	case <-s.beeped:
	case <-time.After(2 * time.Second):
		t.Fatal("the siren never sounded")
	}
}

func (s *audioSpy) snapshot() (maxCalls int, sets, restores []float64, freqs []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxCalls, append([]float64(nil), s.setLevels...),
		append([]float64(nil), s.restoredTo...), append([]int(nil), s.beepFreqs...)
}

// waitFor polls until cond holds, so a test never sleeps for a fixed guess.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestStartSoundsTheSirenAndTakesTheVolume(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(config.AlarmConfig{})
	a.Start()
	defer a.Stop()

	spy.waitForBeep(t)

	if !a.IsPlaying() {
		t.Error("the alarm does not report itself sounding")
	}
	if maxCalls, _, _, _ := spy.snapshot(); maxCalls != 1 {
		t.Errorf("maxVolume called %d times, want once", maxCalls)
	}
}

// The siren alternates between two tones; a single pitch is easy to mistake for
// a notification chime.
func TestSirenAlternatesBetweenTwoTones(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(config.AlarmConfig{})
	a.Start()
	defer a.Stop()

	waitFor(t, "two tones", func() bool {
		_, _, _, freqs := spy.snapshot()
		return len(freqs) >= 2
	})

	_, _, _, freqs := spy.snapshot()
	if freqs[0] != freqHigh {
		t.Errorf("first tone = %d, want %d", freqs[0], freqHigh)
	}
	if freqs[1] != freqLow {
		t.Errorf("second tone = %d, want %d", freqs[1], freqLow)
	}
}

// Starting twice must not stack a second siren on the first, and must not
// overwrite the saved volume with the already-raised one — that would restore
// the machine to full volume on dismissal.
func TestStartWhileAlreadySoundingIsIgnored(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(config.AlarmConfig{})
	a.Start()
	defer a.Stop()
	spy.waitForBeep(t)

	a.Start()

	if maxCalls, _, _, _ := spy.snapshot(); maxCalls != 1 {
		t.Errorf("maxVolume called %d times, want once — the second Start was not ignored", maxCalls)
	}
}

func TestStopRestoresTheVolumeItFound(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)
	spy.currentLevel = 0.42

	a := New(config.AlarmConfig{})
	a.Start()
	spy.waitForBeep(t)
	a.Stop()

	if a.IsPlaying() {
		t.Error("the alarm still reports itself sounding after Stop")
	}
	_, _, restores, _ := spy.snapshot()
	if len(restores) != 1 || restores[0] != 0.42 {
		t.Errorf("restored to %v, want [0.42]", restores)
	}
}

func TestStopOnAnAlarmThatWasNeverStartedDoesNothing(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	New(config.AlarmConfig{}).Stop()

	if _, _, restores, _ := spy.snapshot(); len(restores) != 0 {
		t.Errorf("Stop touched the volume with nothing sounding: %v", restores)
	}
}

// The siren is started before the volume is touched precisely so a failure in
// the audio backend costs the volume rather than the alarm. Silence is the one
// outcome that helps nobody.
func TestAVolumeFailureStillLeavesTheSirenSounding(t *testing.T) {
	spy := newAudioSpy()
	spy.maxErr = errors.New("no audio endpoint")
	spy.install(t)

	a := New(config.AlarmConfig{})
	a.Start()
	defer a.Stop()

	spy.waitForBeep(t)
	if !a.IsPlaying() {
		t.Error("the alarm gave up because the volume could not be raised")
	}
}

// Nothing was saved, so nothing may be restored — writing a zero here would
// leave the machine silent after the alarm was dismissed.
func TestStopAfterAVolumeFailureRestoresNothing(t *testing.T) {
	spy := newAudioSpy()
	spy.maxErr = errors.New("no audio endpoint")
	spy.install(t)

	a := New(config.AlarmConfig{})
	a.Start()
	spy.waitForBeep(t)
	a.Stop()

	if _, _, restores, _ := spy.snapshot(); len(restores) != 0 {
		t.Errorf("restored %v after the volume was never saved", restores)
	}
}

func TestStopAndStartAgainSoundsAFreshSiren(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(config.AlarmConfig{})
	a.Start()
	spy.waitForBeep(t)
	a.Stop()

	a.Start()
	defer a.Stop()
	spy.waitForBeep(t)

	if !a.IsPlaying() {
		t.Error("the alarm did not sound again after being dismissed")
	}
}

func escalating(levels ...config.AlarmLevel) config.AlarmConfig {
	return config.AlarmConfig{EscalationEnabled: true, Levels: levels}
}

// The first escalation step is meant to reach the phone and nothing else, so a
// false alarm in a quiet room does not announce itself to everyone in it.
func TestEscalationPhoneOnlyStepMakesNoSound(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(escalating(config.AlarmLevel{Action: "notify_phone_only"}))
	a.Start()
	defer a.Stop()

	// Nothing to wait for, which is the point; give the goroutine room to run.
	time.Sleep(50 * time.Millisecond)

	maxCalls, sets, _, freqs := spy.snapshot()
	if maxCalls != 0 || len(sets) != 0 {
		t.Errorf("the phone-only step touched the volume: max=%d set=%v", maxCalls, sets)
	}
	if len(freqs) != 0 {
		t.Errorf("the phone-only step sounded the siren: %v", freqs)
	}
}

func TestEscalationMediumStepUsesTheConfiguredVolume(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(escalating(config.AlarmLevel{Action: "medium_volume", VolumePercent: 70}))
	a.Start()
	defer a.Stop()

	spy.waitForBeep(t)

	_, sets, _, _ := spy.snapshot()
	if len(sets) != 1 || sets[0] != 0.7 {
		t.Errorf("set volume to %v, want [0.7]", sets)
	}
}

// A level that names no percentage still has to make a noise. Zero would be a
// silent alarm, which is worse than a wrong guess.
func TestEscalationMediumStepWithNoPercentageFallsBackToHalf(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(escalating(config.AlarmLevel{Action: "medium_volume"}))
	a.Start()
	defer a.Stop()

	spy.waitForBeep(t)

	_, sets, _, _ := spy.snapshot()
	if len(sets) != 1 || sets[0] != 0.5 {
		t.Errorf("set volume to %v, want [0.5]", sets)
	}
}

// Only the level the machine was at before the alarm may be restored. The
// medium step saves it; the full step must not overwrite that with the raised
// value, or dismissing the alarm would leave the machine at medium volume.
func TestEscalationKeepsTheVolumeItFoundFirst(t *testing.T) {
	spy := newAudioSpy()
	spy.currentLevel = 0.2
	spy.install(t)

	a := New(escalating(
		config.AlarmLevel{Action: "medium_volume", VolumePercent: 60},
		config.AlarmLevel{Action: "full_volume"},
	))
	a.Start()

	waitFor(t, "the full-volume step", func() bool {
		maxCalls, _, _, _ := spy.snapshot()
		return maxCalls == 1
	})
	a.Stop()

	_, _, restores, _ := spy.snapshot()
	if len(restores) != 1 || restores[0] != 0.2 {
		t.Errorf("restored to %v, want [0.2] — the level found before the alarm", restores)
	}
}

// Dismissing during a level's delay has to end the chain. Otherwise the alarm
// the owner just silenced comes back by itself a minute later.
func TestStopDuringADelayEndsTheEscalation(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(escalating(
		config.AlarmLevel{DelaySeconds: 30, Action: "full_volume"},
	))
	a.Start()
	a.Stop()

	time.Sleep(50 * time.Millisecond)

	maxCalls, sets, _, freqs := spy.snapshot()
	if maxCalls != 0 || len(sets) != 0 || len(freqs) != 0 {
		t.Errorf("the escalation continued past Stop: max=%d set=%v beeps=%d", maxCalls, sets, len(freqs))
	}
}

// Escalation configured but with no levels in it must not leave the machine
// silent — it falls back to the plain siren.
func TestEscalationWithNoLevelsStillSoundsTheSiren(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(config.AlarmConfig{EscalationEnabled: true})
	a.Start()
	defer a.Stop()

	spy.waitForBeep(t)
	if maxCalls, _, _, _ := spy.snapshot(); maxCalls != 1 {
		t.Errorf("maxVolume called %d times, want once", maxCalls)
	}
}
