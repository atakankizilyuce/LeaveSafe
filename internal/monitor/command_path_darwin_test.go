//go:build darwin

package monitor

import "testing"

func TestSensorQueriesIgnoreUserControlledPATH(t *testing.T) {
	assertIgnoresCommandPath(t, []string{"ioreg", "pmset"}, func() {
		NewInputSensor().Available()
		getIdleSeconds()
		NewLidSensor().Available()
		_, _ = isLidOpenDarwin()
		NewPowerSensor().Available()
		_, _ = isOnACPower()
	})
}
