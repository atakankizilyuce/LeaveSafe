//go:build realtrigger && linux

package realtrigger

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// errUnsupported marks a change this environment cannot produce. It is a reason
// to skip and record the gap, never a reason to fake the result.
var errUnsupported = errors.New("this environment cannot produce the change")

const testAddress = "10.99.99.99/32"

// triggerNetworkChange adds a real address to the loopback interface, which
// changes what net.InterfaceAddrs reports to the sensor.
func triggerNetworkChange(t *testing.T) error {
	t.Helper()

	if err := canSudo(); err != nil {
		return err
	}

	// Clear anything left behind by an earlier run so the add is a real change.
	_, _ = runSudo("ip", "addr", "del", testAddress, "dev", "lo")

	out, err := runSudo("ip", "addr", "add", testAddress, "dev", "lo")
	if err != nil {
		return fmt.Errorf("ip addr add: %w (%s)", err, out)
	}

	t.Cleanup(func() {
		_, _ = runSudo("ip", "addr", "del", testAddress, "dev", "lo")
	})
	return nil
}

// triggerScreenOff is covered by the VM sandbox layer, which owns a real X
// server. A bare runner has no display at all.
func triggerScreenOff(_ *testing.T) error {
	return fmt.Errorf("%w: the Linux runner has no X display (covered by the VM sandbox layer)",
		errUnsupported)
}

// triggerInputActivity is covered by the VM sandbox layer via uinput. A bare
// runner has no /dev/input at all.
func triggerInputActivity(_ *testing.T) error {
	return fmt.Errorf("%w: /dev/input is absent on the runner (covered by the VM sandbox layer)",
		errUnsupported)
}

// canSudo reports whether passwordless sudo is available, which is what these
// triggers need and what a developer workstation often lacks.
func canSudo() error {
	// #nosec G204 -- fixed arguments, no external input
	if out, err := exec.Command("sudo", "-n", "true").CombinedOutput(); err != nil {
		return fmt.Errorf("%w: passwordless sudo is unavailable (%s)",
			errUnsupported, strings.TrimSpace(string(out)))
	}
	return nil
}

func runSudo(args ...string) (string, error) {
	// #nosec G204 -- every argument is a literal defined in this file
	out, err := exec.Command("sudo", append([]string{"-n"}, args...)...).CombinedOutput()
	return string(out), err
}

// unproducibleSensors names what no bare Linux runner can do, with the reason.
func unproducibleSensors() map[string]string {
	return map[string]string{
		"power": "the runner exposes no /sys/class/power_supply entry (covered by the VM sandbox layer)",
		"lid":   "no ACPI lid button exists on the runner or on QEMU x86",
		"usb":   "the runner has no /sys/bus/usb/devices tree (covered by the VM sandbox layer)",
	}
}
