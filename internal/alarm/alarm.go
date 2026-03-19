package alarm

import (
	"bytes"
	"encoding/binary"
	"log"
	"math"
	"sync"
)

const (
	sampleRate    = 44100
	sirenDuration = 2 // seconds per loop iteration
	freqHigh      = 880.0
	freqLow       = 660.0
	switchMs      = 400 // frequency switch interval in ms
)

// Alarm manages the siren sound on the host machine.
type Alarm struct {
	mu      sync.Mutex
	playing bool
	stopCh  chan struct{}
	wavData []byte

	// Original volume level before forcing max (platform-specific).
	savedVolume float64
	volumeSaved bool
}

// New creates a new Alarm instance with pre-generated siren WAV data.
func New() *Alarm {
	return &Alarm{
		wavData: generateSirenWAV(sirenDuration),
	}
}

// Start begins the alarm. It forces system volume to maximum and plays
// the siren in a loop until Stop is called. Start is idempotent.
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

	go a.playLoop()
}

// Stop stops the alarm and restores the previous system volume.
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

	stopPlayback()

	if shouldRestore {
		if err := restoreVolume(saved); err != nil {
			log.Printf("[ALARM] Volume restore error: %v", err)
		}
	}

	log.Println("[ALARM] Alarm stopped")
}

// IsPlaying reports whether the alarm is currently sounding.
func (a *Alarm) IsPlaying() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.playing
}

func (a *Alarm) playLoop() {
	for {
		select {
		case <-a.stopCh:
			return
		default:
			if err := playWAV(a.wavData, a.stopCh); err != nil {
				log.Printf("[ALARM] Playback error: %v", err)
				return
			}
		}
	}
}

// generateSirenWAV creates a two-tone siren WAV in memory.
// The waveform alternates between freqHigh and freqLow every switchMs milliseconds.
func generateSirenWAV(durationSec int) []byte {
	numSamples := sampleRate * durationSec
	dataSize := numSamples * 2 // 16-bit = 2 bytes per sample

	buf := new(bytes.Buffer)
	buf.Grow(44 + dataSize)

	// ── RIFF header ──
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")

	// ── fmt chunk ──
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16)) // chunk size
	binary.Write(buf, binary.LittleEndian, uint16(1))  // PCM format
	binary.Write(buf, binary.LittleEndian, uint16(1))  // mono
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate*2)) // byte rate
	binary.Write(buf, binary.LittleEndian, uint16(2))            // block align
	binary.Write(buf, binary.LittleEndian, uint16(16))           // bits per sample

	// ── data chunk ──
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataSize))

	// ── PCM samples ──
	switchSamples := int(float64(switchMs) / 1000.0 * float64(sampleRate))
	freq := freqHigh

	for i := 0; i < numSamples; i++ {
		if i > 0 && i%switchSamples == 0 {
			if freq == freqHigh {
				freq = freqLow
			} else {
				freq = freqHigh
			}
		}

		// Square wave with slight rounding for less harsh clipping
		t := float64(i) / float64(sampleRate)
		sine := math.Sin(2 * math.Pi * freq * t)
		// Square-ish wave: clip sine to ±0.9 for aggressive alarm sound
		var sample float64
		if sine > 0 {
			sample = 0.9
		} else {
			sample = -0.9
		}

		pcm := int16(sample * 32767)
		binary.Write(buf, binary.LittleEndian, pcm)
	}

	return buf.Bytes()
}
