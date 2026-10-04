//go:build darwin

package audio

import (
	"os"
	"testing"
)

func TestSandboxSilentOutput(t *testing.T) {
	if os.Getenv("LEAVESAFE_SANDBOX_SMOKE") != "1" {
		t.Skip("opt-in signed sandbox smoke")
	}
	dev, err := openOutput(sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := dev.Close(); err != nil {
			t.Error(err)
		}
	}()
	// Exercise the real siren's output with silence, without sounding an alarm.
	for i := 0; i < 5; i++ {
		if err := dev.Write(make([]int16, bufferSamples)); err != nil {
			t.Fatal(err)
		}
	}
}
