package alarm

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

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

	savedVolume float64
	volumeSaved bool

	alarmCfg config.AlarmConfig
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
		safe.Go("alarm-escalation", func() { a.escalationLoop(stop) })
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
	safe.Go("alarm-siren", func() { a.sirenLoop(stop) })

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
		if level.DelaySeconds > 0 {
			select {
			case <-stop:
				return
			case <-time.After(time.Duration(level.DelaySeconds) * time.Second):
			}
		}

		// Check if alarm was stopped during the delay
		a.mu.Lock()
		if !a.playing {
			a.mu.Unlock()
			return
		}
		a.mu.Unlock()

		switch level.Action {
		case "notify_phone_only":
			log.Info("Alarm escalation: phone notification only")
			// No local sound - the hub already sent a WebSocket alert

		case "medium_volume":
			vol := level.VolumePercent
			if vol <= 0 {
				vol = 50
			}
			log.Infof("Alarm escalation: medium volume (%d%%)", vol)
			saved, err := setVolumeFn(float64(vol) / 100.0)
			if err != nil {
				log.Warnf("Volume control error: %v", err)
			} else {
				a.mu.Lock()
				if !a.volumeSaved {
					a.savedVolume = saved
					a.volumeSaved = true
				}
				a.mu.Unlock()
			}
			safe.Go("alarm-siren", func() { a.sirenLoop(stop) })

		case "full_volume":
			log.Info("Alarm escalation: full volume")
			saved, err := maxVolumeFn()
			if err != nil {
				log.Warnf("Volume control error: %v", err)
			} else {
				a.mu.Lock()
				if !a.volumeSaved {
					a.savedVolume = saved
					a.volumeSaved = true
				}
				a.mu.Unlock()
			}
			// sirenLoop may already be running from medium_volume; that's OK,
			// the volume is now at max so the existing siren sounds louder.
		}
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

	log.Info("Alarm stopped")
}

func (a *Alarm) IsPlaying() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.playing
}

func (a *Alarm) sirenLoop(stop chan struct{}) {
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
