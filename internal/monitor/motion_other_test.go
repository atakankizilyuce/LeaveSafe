//go:build !windows

package monitor

import "testing"

func TestMotionUnavailableWithoutPublicPlatformAdapter(t *testing.T) {
	if NewMotionSensor().Available() {
		t.Fatal("unsupported platform claimed motion coverage")
	}
}
