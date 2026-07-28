//go:build realtrigger && darwin

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

const testAddress = "10.99.99.99"

// triggerNetworkChange adds a real alias to the loopback interface, which
// changes what net.InterfaceAddrs reports to the sensor.
func triggerNetworkChange(t *testing.T) error {
	t.Helper()

	if err := canSudo(); err != nil {
		return err
	}

	// Clear anything left behind by an earlier run so the add is a real change.
	_, _ = runSudo("ifconfig", "lo0", "-alias", testAddress)

	out, err := runSudo("ifconfig", "lo0", "alias", testAddress, "255.255.255.255")
	if err != nil {
		return fmt.Errorf("ifconfig alias: %w (%s)", err, out)
	}

	t.Cleanup(func() {
		_, _ = runSudo("ifconfig", "lo0", "-alias", testAddress)
	})
	return nil
}

// triggerScreenOff puts the display to sleep for real, which changes the
// IODisplayWrangler power state the sensor reads.
func triggerScreenOff(_ *testing.T) error {
	// #nosec G204 -- fixed arguments, no external input
	out, err := exec.Command("pmset", "displaysleepnow").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pmset displaysleepnow: %w (%s)", err, out)
	}
	return nil
}

// triggerInputActivity cannot be done here: synthesising HID events needs an
// Accessibility grant that no hosted runner can give.
func triggerInputActivity(_ *testing.T) error {
	return fmt.Errorf("%w: synthetic HID events need an Accessibility grant no hosted runner can give",
		errUnsupported)
}

// canSudo reports whether passwordless sudo is available, which is what the
// network trigger needs and what a developer workstation often lacks.
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

// unproducibleSensors names what no macOS runner can do, with the reason.
func unproducibleSensors() map[string]string {
	return map[string]string{
		"power": "the runner has no battery, so pmset always reports AC power",
		"lid":   "the runner has no clamshell, so AppleClamshellState is absent entirely",
		"usb":   "the runner has no attachable USB device to add or remove",
	}
}
