package audio

import (
	"math"
	"testing"
)

// The siren is the alarm. Everything here is about it being a sound somebody in
// the next room turns towards, and about it being made of numbers that a
// speaker can reproduce without clicking, tearing or falling silent.

// A tone that holds still stops being heard: the ear settles into anything
// steady within seconds, which is the one thing an alarm may not allow. So the
// pitch has to actually move, and move over the whole range it is given.
func TestThePitchSweepsBetweenBothEnds(t *testing.T) {
	s := newSiren()

	low, high := math.Inf(1), math.Inf(-1)
	// Two sweeps' worth, so the fall is seen as well as the climb.
	for range 2 * sweepMs * sampleRate / 1000 {
		f := s.stepSweep()
		low, high = math.Min(low, f), math.Max(high, f)
	}

	if math.Abs(low-lowHz) > 1 {
		t.Errorf("the sweep bottomed out at %.0f Hz, want %d", low, lowHz)
	}
	if math.Abs(high-highHz) > 1 {
		t.Errorf("the sweep topped out at %.0f Hz, want %d", high, highHz)
	}
}

// And it turns round at both ends rather than running off. A sweep that ran
// past the top would carry on into frequencies the speaker cannot make and the
// sampling rate cannot represent, which is a screech and then silence.
func TestTheSweepTurnsRoundAtBothEnds(t *testing.T) {
	s := newSiren()

	// Ten sweeps: far enough that anything that only turns round once, or
	// drifts a little each time, has left the range.
	for range 10 * sweepMs * sampleRate / 1000 {
		if f := s.stepSweep(); f < lowHz-1 || f > highHz+1 {
			t.Fatalf("the sweep reached %.0f Hz, outside %d..%d", f, lowHz, highHz)
		}
	}
}

// The waveform has to stay inside what a sample can hold, give or take the hair
// that scaling by a sampled peak leaves over — which is what the clamp in
// sample is there for. Unclamped, a value over full scale wraps to a large one
// of the opposite sign: a tear through the middle of the sound rather than
// something merely too loud.
func TestTheWaveformStaysInsideWhatASampleCanHold(t *testing.T) {
	for i := range 10000 {
		phase := 2 * math.Pi * float64(i) / 10000
		v := wave(phase)

		if v < -1.01 || v > 1.01 {
			t.Fatalf("the wave reaches %.3f at phase %.3f, well outside -1..1", v, phase)
		}
		if s := sample(v); s != 0 && (v > 0) != (s > 0) {
			t.Fatalf("a wave of %.4f became the sample %d — it wrapped rather than clamping", v, s)
		}
	}
}

// A sine on its own is polite. The third harmonic is what gives the sound the
// edge that carries through a closed door, so it has to actually be in there.
func TestTheWaveIsMoreThanASine(t *testing.T) {
	// Where a sine peaks, the third harmonic is at its trough, so the sum is
	// unmistakably not a sine.
	if got, sine := wave(math.Pi/2), math.Sin(math.Pi/2); math.Abs(got-sine) < 0.1 {
		t.Errorf("the wave is %.3f where a sine is %.3f — there is nothing on top of it", got, sine)
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

// Every buffer is a fresh call, and the wave has to carry on from where the
// last one left off. Starting each from zero puts a step in the waveform every
// twenty milliseconds: fifty clicks a second, which is a sound of its own.
func TestTheWaveCarriesOnAcrossBuffers(t *testing.T) {
	s := newSiren()
	first := make([]int16, bufferSamples)
	second := make([]int16, bufferSamples)

	s.fill(first)
	s.fill(second)

	// A step at the seam is a jump larger than anything inside the buffers.
	biggest := 0
	for i := 1; i < len(second); i++ {
		biggest = max(biggest, abs(int(second[i])-int(second[i-1])))
	}
	if seam := abs(int(second[0]) - int(first[len(first)-1])); seam > biggest {
		t.Errorf("the join between buffers jumps by %d, more than the %d inside one — "+
			"the wave restarted rather than carrying on", seam, biggest)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// The first moment climbs from silence. At full amplitude from the first sample
// the speaker is handed a step, and reproduces it as a click — which at the
// front of an alarm reads as a fault in the machine rather than the start of
// the noise it is making.
func TestTheSoundClimbsFromSilenceRatherThanStarting(t *testing.T) {
	s := newSiren()
	buf := make([]int16, bufferSamples)

	s.fill(buf)

	if buf[0] != 0 {
		t.Errorf("the first sample is %d, want silence", buf[0])
	}
	// And it is at full strength once the attack is over, or the whole siren
	// would be quiet rather than only its first moment.
	loudest := 0
	for _, v := range buf[sampleRate*attackMs/1000:] {
		loudest = max(loudest, abs(int(v)))
	}
	if want := amplitude * 32767 * 0.9; float64(loudest) < want {
		t.Errorf("the loudest sample after the attack is %d, want at least %.0f", loudest, want)
	}
}

// A buffer of silence is a siren nobody hears. This is the assertion that would
// have caught the whole thing being wired to a generator that returns zeroes.
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

// The waveform has to actually fill the range it is given. A sine and its third
// harmonic peak in different places, so their sum reaches about 0.71 rather
// than the 1 the coefficients suggest — and a siren played three decibels
// quieter than asked for is three decibels given away on the one sound in this
// program that exists to be loud.
func TestTheWaveformFillsTheRangeItIsGiven(t *testing.T) {
	peak := peakOf(wave)

	if peak < 0.99 || peak > 1.01 {
		t.Errorf("the waveform peaks at %.3f, want it filling -1..1", peak)
	}
}
