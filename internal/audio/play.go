package audio

import (
	"errors"
	"fmt"
)

// ErrNoOutput is what a machine with nowhere to send sound answers: no audio
// device, no driver, or a platform this build has no backend for.
//
// It is a value the caller is expected to act on rather than only report. The
// alarm falls back to whatever noise it can still make, because a siren that
// gave up because the sound card was busy is a machine being carried away in
// silence.
var ErrNoOutput = errors.New("no audio output available")

// Device is somewhere sound can be sent.
type Device interface {
	// Write plays one buffer of samples. It blocks until the device has room
	// for another, which is what paces the whole thing: without that this would
	// generate sound as fast as the processor allows and hand it over to be
	// queued forever.
	Write(samples []int16) error
	// Close stops the sound immediately and gives the device back.
	Close() error
}

// openOutputFn is the platform's device, as a variable so a test can hand back
// one that makes no sound. Nothing in the running program reassigns it.
var openOutputFn = openOutput

// PlaySiren sounds the siren until stop is closed.
//
// It returns nil when it was stopped, and an error when the machine would not
// make the noise — either because there was nowhere to send it or because the
// device stopped accepting it part way through. Both are the caller's to answer
// for; from here they look the same, and both mean the room is quiet.
func PlaySiren(stop <-chan struct{}) error {
	dev, err := openOutputFn(sampleRate)
	if err != nil {
		return fmt.Errorf("open the audio output: %w", err)
	}
	defer func() { _ = dev.Close() }()

	s := newSiren()
	buf := make([]int16, bufferSamples)
	for {
		select {
		case <-stop:
			return nil
		default:
		}

		s.fill(buf)
		if err := dev.Write(buf); err != nil {
			return fmt.Errorf("play the siren: %w", err)
		}
	}
}

// pcmBytes is the little-endian bytes of a buffer of samples, which is the
// layout every one of these platforms wants and none of them will convert.
func pcmBytes(samples []int16, into []byte) []byte {
	out := into[:0]
	for _, s := range samples {
		// The bits of the sample, not its value: a negative sample is the same
		// sixteen bits either way round, and this is the only thing to do with
		// them that a device understands.
		bits := uint16(s) //nolint:gosec // reinterpreting the bits is the point
		out = append(out, byte(bits), byte(bits>>8))
	}
	return out
}
