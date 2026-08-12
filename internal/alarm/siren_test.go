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

	// sirenErr is what the audio output says when the siren asks to be played.
	// A machine with no sound card answers like this, and the alarm has to fall
	// back to beeping rather than going quiet.
	sirenErr  error
	sirenPlay int
}

func newAudioSpy() *audioSpy {
	return &audioSpy{currentLevel: 0.3, beeped: make(chan struct{}, 64)}
}

// install points the package at this spy for the duration of one test.
func (s *audioSpy) install(t *testing.T) {
	t.Helper()

	realMax, realSet, realRestore, realBeep := maxVolumeFn, setVolumeFn, restoreVolumeFn, beepToneFn
	realSiren := playSirenFn
	t.Cleanup(func() {
		maxVolumeFn, setVolumeFn, restoreVolumeFn, beepToneFn = realMax, realSet, realRestore, realBeep
		playSirenFn = realSiren
	})

	// Replaced before anything can start an alarm. The real one opens the
	// machine's audio device and plays a siren at full volume through it, which
	// is not a thing a test suite may do to whoever is running it.
	playSirenFn = func(stop <-chan struct{}) error {
		s.mu.Lock()
		s.sirenPlay++
		err := s.sirenErr
		s.mu.Unlock()
		if err != nil {
			return err
		}
		select {
		case s.beeped <- struct{}{}:
		default:
		}
		<-stop
		return nil
	}

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

// sirenSnapshot is snapshot with the number of times the siren was played
// through the audio output alongside it.
func (s *audioSpy) sirenSnapshot() (played int, sets, restores []float64, freqs []int) {
	_, sets, restores, freqs = s.snapshot()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sirenPlay, sets, restores, freqs
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

// The siren proper is a waveform played through the machine's audio output.
// That is the whole point of it: the beeps it replaced are a thin single tone
// through whatever the operating system uses for a chime, and the alarm has to
// be the loudest thing in the room rather than the politest.
func TestTheSirenIsPlayedThroughTheAudioOutput(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(config.AlarmConfig{})
	a.Start()
	defer a.Stop()

	spy.waitForBeep(t)

	played, _, _, freqs := spy.sirenSnapshot()
	if played != 1 {
		t.Errorf("the siren was played %d times, want once", played)
	}
	if len(freqs) != 0 {
		t.Errorf("the machine beeped %d times as well as playing the siren", len(freqs))
	}
}

// A machine with no sound card still has to make a noise. The beeps are what is
// left, and they alternate between two tones because a single pitch is easy to
// mistake for a notification chime.
func TestTheFallbackBeepAlternatesBetweenTwoTones(t *testing.T) {
	spy := newAudioSpy()
	spy.sirenErr = errors.New("no audio device in a test")
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

// The guard inside spawn is what makes Stop's wait safe to write at all: a
// goroutine counted after the wait had begun is the one way a WaitGroup can be
// used wrongly, and escalation starts sirens from inside its own goroutine, so a
// late arrival is a real ordering and not a hypothetical one.
func TestNothingIsStartedOnceTheAlarmHasStopped(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(config.AlarmConfig{})
	a.Start()
	spy.waitForBeep(t)
	a.Stop()

	started := make(chan struct{})
	a.spawn("late-siren", func() { close(started) })

	select {
	case <-started:
		t.Fatal("a goroutine was started after the alarm had stopped")
	case <-time.After(50 * time.Millisecond):
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

// A backend that will not set the volume costs the restore, not the escalation.
// The same reasoning as the plain siren: an alarm that gives up because it could
// not get louder has traded the whole point of itself for a tidier exit.
func TestEscalationKeepsSoundingWhenTheVolumeCannotBeSet(t *testing.T) {
	spy := newAudioSpy()
	spy.setErr = errors.New("no audio endpoint")
	spy.install(t)

	a := New(escalating(config.AlarmLevel{Action: "medium_volume", VolumePercent: 70}))
	a.Start()

	spy.waitForBeep(t)
	if !a.IsPlaying() {
		t.Error("the escalation gave up because the volume could not be set")
	}

	a.Stop()

	// Nothing was read, so nothing may be written back. Restoring the zero the
	// failed call returned would leave the machine silent after the dismissal.
	if _, _, restores, _ := spy.snapshot(); len(restores) != 0 {
		t.Errorf("restored %v after the volume was never read", restores)
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

// The delay is the whole shape of an escalation: a step must not act before it
// is due, and must act once it is. Without this the chain could fire every level
// at once and still pass every other test here.
func TestALevelWaitsOutItsDelayAndThenActs(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(escalating(config.AlarmLevel{DelaySeconds: 1, Action: "full_volume"}))
	a.Start()
	defer a.Stop()

	// Well inside the second, so a step that ignored its delay is caught.
	time.Sleep(200 * time.Millisecond)
	if maxCalls, _, _, _ := spy.snapshot(); maxCalls != 0 {
		t.Errorf("the full-volume step ran %d times before its delay was up", maxCalls)
	}

	waitFor(t, "the delayed step", func() bool {
		maxCalls, _, _, _ := spy.snapshot()
		return maxCalls == 1
	})
}

// A restore that fails is reported and then let go. The alarm is already over by
// this point, and there is nothing useful left to do about the machine's volume
// from here — refusing to finish stopping would only leave the alarm stuck.
func TestStopFinishesEvenIfTheVolumeCannotBeRestored(t *testing.T) {
	spy := newAudioSpy()
	spy.restoreErr = errors.New("no audio endpoint")
	spy.install(t)

	a := New(config.AlarmConfig{})
	a.Start()
	spy.waitForBeep(t)
	a.Stop()

	if a.IsPlaying() {
		t.Error("the alarm still reports itself sounding after a failed restore")
	}
	if _, _, restores, _ := spy.snapshot(); len(restores) != 1 {
		t.Errorf("restore attempted %d times, want once", len(restores))
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

// Stop is a report, not a request, and the difference showed up as a data race.
//
// Closing the channel only asks the siren to finish; the loop can still be
// mid-tone, and Stop used to return anyway. So dismissing an alarm said the
// noise had stopped while the machine was still shrieking, and anything the
// caller did next ran alongside a loop that was still going — which is how the
// tests found it, by restoring the audio functions they had swapped in while
// the siren was still reading them.
func TestStopDoesNotReturnWhileTheSirenIsStillSounding(t *testing.T) {
	spy := newAudioSpy()
	// No audio device, so the alarm falls back to beeping — which is the path
	// with a tone in flight to be caught mid-way.
	spy.sirenErr = errors.New("no audio device in a test")
	spy.install(t)

	// Park the siren inside a tone, the way a real 400ms beep does.
	sounding := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	beepToneFn = func(int, int, <-chan struct{}) {
		once.Do(func() { close(sounding) })
		<-release
	}

	a := New(config.AlarmConfig{})
	a.Start()
	<-sounding

	stopped := make(chan struct{})
	go func() {
		a.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop returned while the siren was still sounding")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop never returned once the siren had finished")
	}
}

// Escalation starts its siren from inside its own goroutine, which is the case
// where waiting for the loops is easiest to get wrong: a siren arriving after
// Stop has begun waiting would either be missed or panic the wait.
func TestStopWaitsForAnEscalatingAlarmToo(t *testing.T) {
	spy := newAudioSpy()
	spy.install(t)

	a := New(config.AlarmConfig{
		EscalationEnabled: true,
		Levels: []config.AlarmLevel{
			{Action: "medium_volume", VolumePercent: 50},
			{Action: "full_volume"},
		},
	})
	a.Start()
	spy.waitForBeep(t)

	done := make(chan struct{})
	go func() {
		a.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop never returned on an escalating alarm")
	}
	if a.IsPlaying() {
		t.Error("the alarm still reports itself sounding after Stop")
	}
}
