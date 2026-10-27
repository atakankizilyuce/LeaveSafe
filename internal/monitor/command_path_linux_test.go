//go:build linux

package monitor

import "testing"

func TestScreenQueryIgnoresUserControlledPATH(t *testing.T) {
	assertIgnoresCommandPath(t, []string{"xset"}, func() {
		NewScreenSensor().Available()
		_, _ = isScreenOnLinux()
	})
}
