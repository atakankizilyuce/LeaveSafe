package alarm

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/audio"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/safe"
)

const (
	freqHigh = 880 // Hz
	freqLow  = 660 // Hz
	switchMs = 400 // frequency alternation interval in ms
)

// The audio backends reach the operating system: COM vtables on Windows, a raw
// C pointer on Linux, a helper process on macOS. Calling them from a test would
// mean a machine that genuinely shrieks at full volume every time the suite
// runs, so every call goes through these and a test swaps them.
//
// Nothing in the running program reassigns them. They are variables only so the
// tests can see what Start and Stop asked the system to do.
var (
	maxVolumeFn     = maxVolume
	setVolumeFn     = setVolume
	restoreVolumeFn = restoreVolume
	beepToneFn      = beepTone
	playSirenFn     = audio.PlaySiren
)

// clampLevel keeps a volume scalar inside the 0..1 range every platform's audio
// API expects. Out-of-range values are rejected by some backends and silently
// wrapped by others, so they are clamped once here rather than trusted.
func clampLevel(level float64) float64 {
	if level < 0 {
		return 0
	}
	if level > 1 {
		return 1
	}
	return level
}

// Alarm manages the siren sound on the host machine.
type Alarm struct {
	mu      sync.Mutex
	playing bool
	stopCh  chan struct{}

	// loops counts the goroutines this alarm started, so Stop can wait for them
	// rather than only asking them to finish.
	//
	// Closing the channel is a request, and Stop used to return the moment it
	// had made one — with a siren still mid-tone behind it. On a machine being
	// carried away that is the wrong report to make: dismissing the alarm said
	// the noise had stopped while it had not. It also left the loops running
	// through whatever the caller did next, which is how the tests caught it:
	// restoring the audio functions they had swapped in raced with a loop still
	// reading them.
	loops sync.WaitGroup

	savedVolume float64
	volumeSaved bool

	alarmCfg config.AlarmConfig
}

// spawn starts one of the alarm's own goroutines and counts it.
//
// Refusing to start once the alarm has stopped is what makes the counting safe.
// Escalation starts a siren from inside its own goroutine, so without this a
// late arrival could add to the count after Stop had already begun waiting on
// it — which is the one way a WaitGroup can be used wrongly.
func (a *Alarm) spawn(name string, fn func()) {
	a.mu.Lock()
	if !a.playing {
		a.mu.Unlock()
		return
	}
	a.loops.Add(1)
	a.mu.Unlock()

	safe.Go(name, func() {
		defer a.loops.Done()
		fn()
	})
}

// New creates a new Alarm instance with the given alarm configuration.
func New(cfg config.AlarmConfig) *Alarm {
	return &Alarm{alarmCfg: cfg}
}

// Start activates the alarm. If escalation is enabled, the alarm will
// progress through the configured levels. Otherwise it behaves as before
// (immediate full volume siren).
func (a *Alarm) Start() {
	a.mu.Lock()
	if a.playing {
		a.mu.Unlock()
		return
	}
	a.playing = true
	stop := make(chan struct{})
	a.stopCh = stop
	a.mu.Unlock()

	// The loops are handed the channel this run created rather than reading
	// a.stopCh as they go. Reading the field was a data race — it is written
	// here under the lock and was read without one — and the race had a real
	// shape: a siren parked inside beepTone through a Stop and a following
	// Start would wake, read the *new* channel, and so never see that its own
	// was closed. It would sound past the dismissal, with the new siren
	// alongside it, and nothing left could stop it.
	if a.alarmCfg.EscalationEnabled && len(a.alarmCfg.Levels) > 0 {
		a.spawn("alarm-escalation", func() { a.escalationLoop(stop) })
	} else {
		a.startFullSiren(stop)
	}
}

func (a *Alarm) startFullSiren(stop chan struct{}) {
	// The siren is launched before the volume is touched, and the order is
	// load-bearing. The volume backends walk COM vtables on Windows and a raw C
	// pointer on Linux; a panic in one of them is recovered by the caller, which
	// with the old order meant Start had already set playing to true and had not
	// yet started any siren. The alarm then reported itself sounding, made no
	// noise, and refused to start again until something called Stop — while the
	// machine it was guarding was being carried away.
	//
	// This way a failure down there costs the volume, not the alarm. A siren at
	// whatever volume the laptop was already at is the whole point; silence is
	// the one outcome that helps nobody.
	log.Info("Siren started")
	a.spawn("alarm-siren", func() { a.sirenLoop(stop) })

	log.Info("Forcing system volume to maximum")
	saved, err := maxVolumeFn()
	if err != nil {
		log.Warnf("Volume control error: %v", err)
		return
	}
	a.mu.Lock()
	a.savedVolume = saved
	a.volumeSaved = true
	a.mu.Unlock()
}

func (a *Alarm) escalationLoop(stop chan struct{}) {
	for _, level := range a.alarmCfg.Levels {
		if !a.waitForLevel(level, stop) {
			return
		}
		a.applyLevel(level, stop)
	}
}

// waitForLevel holds off until this step of the chain is due, and reports
// whether the alarm is still sounding when it is.
//
// Both halves are needed. The wait watches the stop channel so a dismissal
// during a thirty-second delay does not have to be waited out; the check that
// follows catches a dismissal that landed while the timer was already firing.
func (a *Alarm) waitForLevel(level config.AlarmLevel, stop chan struct{}) bool {
	if level.DelaySeconds > 0 {
		select {
		case <-stop:
			return false
		case <-time.After(time.Duration(level.DelaySeconds) * time.Second):
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.playing
}

// applyLevel does what one step of the escalation chain asks for. An action this
// build does not know is ignored rather than treated as an error: config.Validate
// has already had its say, and an alarm mid-escalation is the wrong place to
// start refusing things.
func (a *Alarm) applyLevel(level config.AlarmLevel, stop chan struct{}) {
	switch level.Action {
	case "notify_phone_only":
		log.Info("Alarm escalation: phone notification only")
		// No local sound - the hub already sent a WebSocket alert

	case "medium_volume":
		a.escalateToMediumVolume(level, stop)

	case "full_volume":
		a.escalateToFullVolume()
	}
}

func (a *Alarm) escalateToMediumVolume(level config.AlarmLevel, stop chan struct{}) {
	vol := level.VolumePercent
	if vol <= 0 {
		vol = 50
	}
	log.Infof("Alarm escalation: medium volume (%d%%)", vol)

	saved, err := setVolumeFn(float64(vol) / 100.0)
	a.rememberVolume(saved, err)

	a.spawn("alarm-siren", func() { a.sirenLoop(stop) })
}

func (a *Alarm) escalateToFullVolume() {
	log.Info("Alarm escalation: full volume")

	saved, err := maxVolumeFn()
	a.rememberVolume(saved, err)

	// sirenLoop may already be running from medium_volume; that's OK,
	// the volume is now at max so the existing siren sounds louder.
}

// rememberVolume records what the volume was before this step raised it, so Stop
// can put it back.
//
// Only the first step through gets to record it. The chain raises the volume
// more than once — medium and then full — and each backend hands back what the
// volume was immediately before that call, so a later step would overwrite the
// only reading taken before the alarm touched anything. Restoring would then
// leave the machine at the alarm's own medium volume rather than the owner's.
//
// A backend that fails costs the restore, not the escalation: the siren is the
// point, and refusing to get louder because the volume could not be read would
// trade the whole alarm for a tidier exit.
func (a *Alarm) rememberVolume(saved float64, err error) {
	if err != nil {
		log.Warnf("Volume control error: %v", err)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.volumeSaved {
		a.savedVolume = saved
		a.volumeSaved = true
	}
}

// Stop stops the alarm and restores the previous volume level.
func (a *Alarm) Stop() {
	a.mu.Lock()
	if !a.playing {
		a.mu.Unlock()
		return
	}
	a.playing = false
	close(a.stopCh)

	shouldRestore := a.volumeSaved
	saved := a.savedVolume
	a.volumeSaved = false
	a.mu.Unlock()

	if shouldRestore {
		if err := restoreVolumeFn(saved); err != nil {
			log.Warnf("Volume restore error: %v", err)
		}
	}

	// Bounded by one tone. Both beep backends select on the same channel that
	// was just closed, and the Windows one waits only for the call already in
	// flight — so this is the length of a beep at worst, and it is the
	// difference between "stopped" and "asked to stop".
	a.loops.Wait()

	log.Info("Alarm stopped")
}

func (a *Alarm) IsPlaying() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.playing
}

// sirenLoop makes the noise, by the best means this machine offers.
//
// The siren proper is generated sample by sample and played through the audio
// device, which is what makes it a sound somebody in the next room turns
// towards. Where there is no device to play it through — no sound card, a
// driver that will not open, or a machine with no libasound on it — it falls
// back to the beeps, which are thin and are better than nothing.
//
// Falling back rather than reporting and stopping is the whole point. This runs
// while a laptop is being carried away; silence is the one outcome that helps
// nobody, and the machine that cannot open its sound card is exactly the
// machine whose owner is not there to be told why.
func (a *Alarm) sirenLoop(stop chan struct{}) {
	if err := playSirenFn(stop); err != nil {
		log.Warnf("Siren: %v — falling back to the console beep", err)
		a.beepLoop(stop)
	}
}

// beepLoop alternates two tones through whatever the operating system will beep
// with. It is what there was before there was a siren, and it is what is left
// when the siren cannot be played.
func (a *Alarm) beepLoop(stop chan struct{}) {
	high := true
	for {
		select {
		case <-stop:
			return
		default:
			freq := freqLow
			if high {
				freq = freqHigh
			}
			high = !high
			beepToneFn(freq, switchMs, stop)
		}
	}
}
