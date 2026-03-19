package alarm

import (
	"log"
	"sync"
)

const (
	freqHigh = 880  // Hz
	freqLow  = 660  // Hz
	switchMs = 400  // frequency alternation interval in ms
)

// Alarm manages the siren sound on the host machine.
type Alarm struct {
	mu      sync.Mutex
	playing bool
	stopCh  chan struct{}

	savedVolume float64
	volumeSaved bool
}

// New creates a new Alarm instance.
func New() *Alarm {
	return &Alarm{}
}

// Start forces system volume to maximum and plays the siren until Stop is called.
func (a *Alarm) Start() {
	a.mu.Lock()
	if a.playing {
		a.mu.Unlock()
		return
	}
	a.playing = true
	a.stopCh = make(chan struct{})
	a.mu.Unlock()

	log.Println("[ALARM] Forcing system volume to maximum")
	saved, err := maxVolume()
	if err != nil {
		log.Printf("[ALARM] Volume control error: %v", err)
	} else {
		a.mu.Lock()
		a.savedVolume = saved
		a.volumeSaved = true
		a.mu.Unlock()
	}

	log.Println("[ALARM] Siren started")
	go a.sirenLoop()
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
			log.Printf("[ALARM] Volume restore error: %v", err)
		}
	}

	log.Println("[ALARM] Alarm stopped")
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
