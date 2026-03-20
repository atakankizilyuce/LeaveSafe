package alarm

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/config"
)

const (
	freqHigh = 880 // Hz
	freqLow  = 660 // Hz
	switchMs = 400 // frequency alternation interval in ms
)

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
	a.stopCh = make(chan struct{})
	a.mu.Unlock()

	if a.alarmCfg.EscalationEnabled && len(a.alarmCfg.Levels) > 0 {
		go a.escalationLoop()
	} else {
		a.startFullSiren()
	}
}

func (a *Alarm) startFullSiren() {
	log.Info("Forcing system volume to maximum")
	saved, err := maxVolume()
	if err != nil {
		log.Warnf("Volume control error: %v", err)
	} else {
		a.mu.Lock()
		a.savedVolume = saved
		a.volumeSaved = true
		a.mu.Unlock()
	}

	log.Info("Siren started")
	go a.sirenLoop()
}

func (a *Alarm) escalationLoop() {
	for _, level := range a.alarmCfg.Levels {
		if level.DelaySeconds > 0 {
			select {
			case <-a.stopCh:
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
			saved, err := setVolume(float64(vol) / 100.0)
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
			go a.sirenLoop()

		case "full_volume":
			log.Info("Alarm escalation: full volume")
			saved, err := maxVolume()
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
		if err := restoreVolume(saved); err != nil {
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

func (a *Alarm) sirenLoop() {
	high := true
	for {
		select {
		case <-a.stopCh:
			return
		default:
			freq := freqLow
			if high {
				freq = freqHigh
			}
			high = !high
			beepTone(freq, switchMs, a.stopCh)
		}
	}
}
