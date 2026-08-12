package audio

import (
	"math"
	"testing"
)

// The siren is the alarm. Everything here is about it being a sound somebody in
// the next room turns towards, and about it being made of numbers a speaker can
// reproduce without clicking, tearing or falling silent.

// The two pairs have to actually alternate. Held on one, this is a chime with a
// wrong note in it; alternating, it is a sound that never settles — which is
// the whole reason there are two.
func TestTheTwoPairsAlternate(t *testing.T) {
	firstLow, firstHigh := notes(0)
	secondLow, secondHigh := notes(1)

	if firstLow == secondLow || firstHigh == secondHigh {
		t.Fatalf("both notes play %.0f/%.0f — there is only one pair", firstLow, firstHigh)
	}
	if low, high := notes(2); low != firstLow || high != firstHigh {
		t.Errorf("the third note is %.0f/%.0f, want a return to the first pair", low, high)
	}
	if low, high := notes(3); low != secondLow || high != secondHigh {
		t.Errorf("the fourth note is %.0f/%.0f, want the second pair again", low, high)
	}
}

// Each pair has to be dissonant. A consonant interval is a chord, and a chord
// is a notification sound — the ear files it away as something that has already
// happened rather than something happening now.
func TestBothPairsAreDissonant(t *testing.T) {
	for index := range 2 {
		low, high := notes(index)

		// The ratio between them, in semitones. A tritone is six; anything
		// within half a semitone of a fifth (7) or a fourth (5) has resolved
		// into music.
		semitones := 12 * math.Log2(high/low)
		if math.Abs(semitones-6) > 1 {
			t.Errorf("pair %d is %.0f/%.0f, which is %.1f semitones apart — that is an interval, not a clash",
				index, low, high, semitones)
		}
	}
}

// A note that starts or stops at full amplitude hands the speaker a step, and a
// step is a click. Ten notes a second means ten clicks a second on top of the
// sound rather than part of it.
func TestNothingInTheWaveformIsAStep(t *testing.T) {
	s := newSiren()
	// Several notes' worth, so the joins between them are included.
	buf := make([]int16, sampleRate*noteMs*3/1000)
	s.fill(buf)

	// The steepest the sound itself can be: both notes rising together, through
	// a clipper that steepens them, at full amplitude. A click is many times
	// this — a step of a whole note's worth of level in one sample.
	const steepest = 9000

	for i := 1; i < len(buf); i++ {
		if jump := abs(int(buf[i]) - int(buf[i-1])); jump > steepest {
			t.Fatalf("the waveform jumps by %d between samples %d and %d, which is a click",
				jump, i-1, i)
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Every buffer is a fresh call, and the sound has to carry on from where the
// last one left off. Restarting the oscillators would put a discontinuity every
// twenty milliseconds — fifty clicks a second, which is a sound of its own.
func TestTheWaveCarriesOnAcrossBuffers(t *testing.T) {
	s := newSiren()
	first := make([]int16, bufferSamples)
	second := make([]int16, bufferSamples)

	s.fill(first)
	s.fill(second)

	biggest := 0
	for i := 1; i < len(second); i++ {
		biggest = max(biggest, abs(int(second[i])-int(second[i-1])))
	}
	if seam := abs(int(second[0]) - int(first[len(first)-1])); seam > biggest {
		t.Errorf("the join between buffers jumps by %d, more than the %d inside one — "+
			"the sound restarted rather than carrying on", seam, biggest)
	}
}

// The first sample is silence, so the alarm does not open with a click — which
// reads as a fault in the machine rather than the start of the noise it is
// making.
func TestTheSoundClimbsFromSilenceRatherThanStarting(t *testing.T) {
	s := newSiren()
	buf := make([]int16, bufferSamples)

	s.fill(buf)

	if buf[0] != 0 {
		t.Errorf("the first sample is %d, want silence", buf[0])
	}
}

// And it reaches most of what a sample can hold. This is the assertion that
// catches the whole thing being quietly wired to something polite: an alarm at
// half amplitude is six decibels given away on the one sound in this program
// that exists to be loud.
func TestTheSirenIsLoud(t *testing.T) {
	s := newSiren()
	buf := make([]int16, sampleRate*noteMs/1000)
	s.fill(buf)

	loudest := 0
	for _, v := range buf {
		loudest = max(loudest, abs(int(v)))
	}
	if want := amplitude * (1 - roughDepth) * 32767; float64(loudest) < want {
		t.Errorf("the loudest sample is %d, want at least %.0f", loudest, want)
	}
}

func TestABufferIsNotSilence(t *testing.T) {
	s := newSiren()
	buf := make([]int16, bufferSamples)

	s.fill(buf)

	for _, v := range buf {
		if v != 0 {
			return
		}
	}
	t.Error("a whole buffer of siren is silence")
}

// ---- the pieces ----------------------------------------------------------

// The clipper is what makes two sine waves into something with an edge on it.
// It has to add level in the middle — that is the harmonics — without ever
// passing full scale, which would arrive at the clamp and be heard as a tear.
func TestTheClipperAddsEdgeWithoutPassingFullScale(t *testing.T) {
	if got := softClip(1); math.Abs(got-1) > 0.001 {
		t.Errorf("softClip(1) = %.4f, want it to reach full scale", got)
	}
	if got := softClip(-1); math.Abs(got+1) > 0.001 {
		t.Errorf("softClip(-1) = %.4f, want it to reach full scale the other way", got)
	}
	if got := softClip(0); got != 0 {
		t.Errorf("softClip(0) = %.4f, want silence to stay silent", got)
	}

	// Louder in the middle than it went in, which is the harmonics being added
	// rather than the level being turned up.
	if got := softClip(0.5); got <= 0.5 {
		t.Errorf("softClip(0.5) = %.4f, want more than 0.5 — nothing was added", got)
	}

	for i := range 2001 {
		level := float64(i)/1000 - 1
		if got := softClip(level); got < -1 || got > 1 {
			t.Fatalf("softClip(%.3f) = %.4f, outside what a sample can hold", level, got)
		}
	}
}

// The fade at each end of a note is silent at the edges and out of the way in
// the middle. A fade that never reached 1 would be a siren quieter than it
// asked to be, for no reason anybody could hear.
func TestANoteFadesInAndOut(t *testing.T) {
	const length, over = 1000, 10

	if got := ramp(0, length, over); got != 0 {
		t.Errorf("the note starts at %.2f, want silence", got)
	}
	if got := ramp(length, length, over); got != 0 {
		t.Errorf("the note ends at %.2f, want silence", got)
	}
	if got := ramp(length/2, length, over); got != 1 {
		t.Errorf("the middle of the note is at %.2f, want full", got)
	}
}

// The roughness is what makes it harsh rather than merely loud. It has to
// actually move, and it must never reach silence: a modulation that closed
// completely would turn one sound into a series of them, which is a different
// alarm and a politer one.
func TestTheRoughnessWobblesWithoutEverGoingQuiet(t *testing.T) {
	lowest, highest := math.Inf(1), math.Inf(-1)
	// A full cycle of the modulation.
	for at := range sampleRate / roughHz {
		v := rough(at, sampleRate)
		lowest, highest = math.Min(lowest, v), math.Max(highest, v)
	}

	if highest < 0.99 {
		t.Errorf("the roughness never opens past %.2f, so the siren never reaches full", highest)
	}
	if lowest > 0.99 {
		t.Error("the roughness never closes, so there is no roughness")
	}
	if want := 1 - roughDepth; math.Abs(lowest-want) > 0.01 {
		t.Errorf("the roughness closes to %.2f, want %.2f — deeper than this is a stutter", lowest, want)
	}
}

func TestASampleIsClampedRatherThanWrapped(t *testing.T) {
	cases := map[string]struct {
		level float64
		want  int16
	}{
		"silence":        {0, 0},
		"full scale":     {1, 32767},
		"full the other": {-1, -32767},
		"over the top":   {1.5, 32767},
		"under it":       {-1.5, -32767},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := sample(tc.level); got != tc.want {
				t.Errorf("sample(%v) = %d, want %d", tc.level, got, tc.want)
			}
		})
	}
}
