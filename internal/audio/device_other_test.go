//go:build !windows

package audio

import (
	"errors"
	"testing"
)

// The gap, stated as a test. macOS and Linux have no backend yet, so the siren
// cannot be played there and the alarm falls back to the beeps it was making
// before — which is thin, and is the next change. Written down here so that the
// day a backend arrives, this test is what has to be deleted rather than
// something that quietly keeps passing.
func TestThereIsNoAudioOutputOnThisPlatformYet(t *testing.T) {
	dev, err := openOutput(sampleRate)

	if !errors.Is(err, ErrNoOutput) {
		t.Errorf("openOutput = %v, want it to report that there is none", err)
	}
	if dev != nil {
		t.Error("a device was handed back by a platform that has no backend")
	}
}
