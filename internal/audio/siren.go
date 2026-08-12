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

const (
	// sampleRate is 44.1 kHz, which every audio device made in the last thirty
	// years accepts without conversion. A siren has no need of more.
	sampleRate = 44100

	// The sweep. A tone that holds still is easy to stop hearing — the ear
	// settles into anything steady within seconds, which is exactly what an
	// alarm must not allow. So it climbs and falls between these, the way a
	// siren does, and never sits anywhere long enough to become background.
	lowHz   = 640
	highHz  = 1400
	sweepMs = 550

	// amplitude leaves a little headroom below full scale. At full scale the
	// sum of the harmonics below would clip, and clipping is a crackle rather
	// than a louder siren.
	amplitude = 0.82

	// attackMs is the fade the first moment of sound gets. Starting at full
	// amplitude puts a step into the waveform, which a speaker reproduces as a
	// click — and a click at the front of an alarm reads as a fault in the
	// machine rather than the beginning of the noise it is making.
	attackMs = 12

	// bufferMs is how much sound is handed over at a time. It is the delay
	// between the alarm being dismissed and the room going quiet, so it is
	// short; short enough to be missed if the machine stalls, so not shorter.
	bufferMs = 20
)

// bufferSamples is how many samples one handover carries.
const bufferSamples = sampleRate * bufferMs / 1000

// siren generates the waveform one buffer at a time.
//
// It carries its phase and its place in the sweep from one buffer to the next.
// Starting each buffer from zero would put a discontinuity every twenty
// milliseconds — fifty clicks a second, which is a sound of its own and not the
// one intended.
type siren struct {
	rate int

	// phase is where the wave is, in radians, and swept is where the sweep is,
	// from 0 at the low tone to 1 at the high one.
	phase  float64
	swept  float64
	rising bool

	// played counts the samples so far, for the attack.
	played int
}

func newSiren() *siren {
	return &siren{rate: sampleRate, rising: true}
}

// fill writes the next stretch of siren into buf.
func (s *siren) fill(buf []int16) {
	for i := range buf {
		freq := s.stepSweep()

		// Advanced by the frequency of this sample rather than by a whole
		// buffer at a time, which is what makes a sweep a sweep instead of a
		// staircase of tones.
		s.phase += 2 * math.Pi * freq / float64(s.rate)
		if s.phase > 2*math.Pi {
			s.phase -= 2 * math.Pi
		}

		buf[i] = sample(wave(s.phase) * amplitude * s.attack())
		s.played++
	}
}

// stepSweep moves the sweep on by one sample and returns the frequency to use
// for it.
func (s *siren) stepSweep() float64 {
	step := 1000 / (float64(s.rate) * float64(sweepMs))

	if s.rising {
		s.swept += step
		if s.swept >= 1 {
			s.swept, s.rising = 1, false
		}
	} else {
		s.swept -= step
		if s.swept <= 0 {
			s.swept, s.rising = 0, true
		}
	}
	return lowHz + (highHz-lowHz)*s.swept
}

// attack is the gain to apply to this sample, which climbs from silence over
// the first few milliseconds and is 1 from then on.
func (s *siren) attack() float64 {
	over := s.rate * attackMs / 1000
	if s.played >= over {
		return 1
	}
	return float64(s.played) / float64(over)
}

// wave is the shape of one cycle, scaled to fill the range -1 to 1.
//
// A sine and its third harmonic rather than a sine alone: it is the beginning
// of a square wave, and the edge that adds is what makes the sound carry
// through a closed door and a room of people talking. A pure tone at the same
// amplitude is politely ignorable, which is the one thing this must not be.
func wave(phase float64) float64 {
	return rawWave(phase) / wavePeak
}

// rawWave is that sum before it is scaled.
func rawWave(phase float64) float64 {
	const (
		fundamental = 0.8
		third       = 0.2
	)
	return fundamental*math.Sin(phase) + third*math.Sin(3*phase)
}

// wavePeak is the largest value rawWave takes, measured once by walking a
// cycle.
//
// Two waves added together do not peak at the sum of their amplitudes, because
// they peak in different places: this pair reaches about 0.71 rather than the 1
// its coefficients suggest. Without dividing it out the siren would play at
// three-fifths of the level it asks for — three decibels of alarm given away
// for nothing, on the one sound in this program that exists to be loud.
var wavePeak = peakOf(rawWave)

// peakOf walks one cycle and reports the largest excursion it finds. A sampled
// walk can pass either side of the true maximum, so what it returns is a hair
// low and the waveform a hair over full scale — which is what the clamp in
// sample is for.
func peakOf(f func(float64) float64) float64 {
	const steps = 4096

	peak := 0.0
	for i := range steps {
		peak = math.Max(peak, math.Abs(f(2*math.Pi*float64(i)/steps)))
	}
	return peak
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
