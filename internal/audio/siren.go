// Package audio makes the noise the alarm is for.
//
// It exists because there was not one. On Windows the alarm called kernel32's
// Beep, which is a single square tone at whatever the machine feels like and
// stops for as long as it takes to ask for the next one; everywhere else it
// printed a bell character and hoped the terminal would do something with it —
// which in a terminal nobody is looking at, or a run with no terminal at all,
// is silence. The phone shrieked and the laptop did not, which is backwards: it
// is the laptop somebody is walking away with, and the room it is in is where
// the noise has to be.
//
// So the sound is generated here, sample by sample, and handed to the machine's
// own audio output. Everything about what it sounds like is in this file and is
// ordinary arithmetic; the platform files below it do nothing but carry the
// numbers to a device.
package audio

import "math"

// What the siren is.
//
// Two dissonant pairs of notes, slamming back and forth five times a second,
// with no gap between them and hard edges on both.
//
// It is deliberately not a sweep. A tone gliding up and down is what every
// emergency vehicle in the world does, so a laptop making that sound reads as
// an ambulance somewhere outside rather than as this machine, in this room,
// now — the one thing the alarm cannot afford to be mistaken for. What is left
// once the sweep is gone has to carry the urgency by other means, and there are
// three of them here: an interval the ear hears as wrong, a modulation fast
// enough to be heard as harshness rather than as a beat, and edges sharp enough
// that nothing about it sounds like it is passing by.
const (
	// sampleRate is 44.1 kHz, which every audio device made in the last thirty
	// years accepts without conversion. A siren has no need of more.
	sampleRate = 44100

	// The two pairs. Each is close to a tritone — the interval that has been
	// called the devil's own since the fifteenth century, for the same reason
	// it is useful here: it does not resolve, and the ear keeps waiting for it
	// to. Alternating between two of them denies it even the comfort of a
	// steady wrongness.
	lowA, highA = 622.0, 880.0
	lowB, highB = 740.0, 1046.0

	// noteMs is how long each pair holds. Five changes a second: fast enough
	// that it is plainly an alarm rather than a chime, slow enough that each
	// note is a note rather than part of a texture.
	noteMs = 190

	// edgeMs is the fade at each end of a note. Without it every change is a
	// step in the waveform, which a speaker reproduces as a click — ten clicks
	// a second, on top of the sound rather than part of it.
	edgeMs = 4

	// The roughness. Amplitude wobbling twenty times a second is past the point
	// where the ear counts the beats and into where it calls the sound harsh,
	// which is the quality that makes an alarm hard to sit through.
	roughHz    = 20
	roughDepth = 0.3

	// drive is how hard the mixed notes are pushed into the soft clipper. Above
	// one it adds harmonics, and the harmonics are what get through a closed
	// door: the alternative way to be louder is amplitude, and there is none of
	// that left.
	drive = 1.8

	// amplitude leaves a little headroom below full scale, so that nothing
	// arrives at the clamp in sample and is heard as a tear.
	amplitude = 0.9

	// bufferMs is how much sound is handed over at a time. It is the delay
	// between the alarm being dismissed and the room going quiet, so it is
	// short; short enough to be missed if the machine stalls, so not shorter.
	bufferMs = 20
)

// bufferSamples is how many samples one handover carries.
const bufferSamples = sampleRate * bufferMs / 1000

// siren generates the waveform one buffer at a time.
//
// The two oscillators run continuously, whether or not the note they are
// playing is the one being heard. Restarting them at each change would put a
// discontinuity at every note edge and at every buffer seam — which is the
// clicking this is shaped to avoid.
type siren struct {
	rate   int
	played int
	a, b   voice
}

func newSiren() *siren {
	return &siren{rate: sampleRate, a: voice{rate: sampleRate}, b: voice{rate: sampleRate}}
}

// fill writes the next stretch of siren into buf.
func (s *siren) fill(buf []int16) {
	note := s.rate * noteMs / 1000

	for i := range buf {
		low, high := notes(s.played / note)
		mix := (s.a.next(low) + s.b.next(high)) / 2

		buf[i] = sample(softClip(mix) * amplitude * s.envelope(s.played%note, note))
		s.played++
	}
}

// notes is the pair being played, which alternates every note.
func notes(index int) (low, high float64) {
	if index%2 == 1 {
		return lowB, highB
	}
	return lowA, highA
}

// envelope is how loud this sample is: faded at both ends of its note, and
// wobbling throughout.
func (s *siren) envelope(at, note int) float64 {
	return ramp(at, note, s.rate*edgeMs/1000) * rough(s.played, s.rate)
}

// voice is one oscillator, carrying its phase from one sample to the next.
type voice struct {
	rate  int
	phase float64
}

func (v *voice) next(freq float64) float64 {
	v.phase += 2 * math.Pi * freq / float64(v.rate)
	if v.phase > 2*math.Pi {
		v.phase -= 2 * math.Pi
	}
	return math.Sin(v.phase)
}

// ramp is a linear fade at both ends of a note, so that a note starting and
// stopping is not two clicks with a sound between them.
func ramp(at, length, over int) float64 {
	switch {
	case at < over:
		return float64(at) / float64(over)
	case at > length-over:
		return float64(length-at) / float64(over)
	default:
		return 1
	}
}

// rough is the amplitude modulation, which never reaches silence: it is meant
// to be heard as texture on one sound rather than as a series of sounds.
func rough(at, rate int) float64 {
	phase := 2 * math.Pi * roughHz * float64(at) / float64(rate)
	return 1 - roughDepth*(1-math.Cos(phase))/2
}

// softClip pushes the waveform towards a square without ever passing full
// scale.
//
// Two sine waves added together are a polite sound, and politeness is the one
// thing this must not be. Driving them into a curve that flattens as it
// approaches the limit adds the harmonics that let a sound cut through a closed
// door and a room of people talking — and does it without the hard clipping
// that would arrive at the clamp in sample and be heard as a tear rather than
// as an edge.
func softClip(level float64) float64 {
	return math.Tanh(drive*level) / math.Tanh(drive)
}

// sample turns a level between -1 and 1 into the signed 16-bit number a device
// expects, clamped rather than allowed to wrap.
//
// Wrapping is the failure worth naming: a value a little over full scale
// becomes a large negative one, so a moment too loud comes out as a tear
// through the middle of the waveform rather than as distortion.
func sample(level float64) int16 {
	const full = 32767
	switch {
	case level >= 1:
		return full
	case level <= -1:
		return -full
	default:
		return int16(level * full)
	}
}
