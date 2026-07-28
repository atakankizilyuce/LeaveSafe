//go:build realtrigger && windows

package realtrigger

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// errUnsupported marks a change this environment cannot produce. It is a reason
// to skip and record the gap, never a reason to fake the result.
var errUnsupported = errors.New("this environment cannot produce the change")

const loopbackAdapter = "Loopback Pseudo-Interface 1"

// triggerNetworkChange adds a real IP address to the loopback adapter, which
// changes what net.InterfaceAddrs reports to the sensor.
func triggerNetworkChange(t *testing.T) error {
	t.Helper()

	// Clear any address left behind by an earlier run so the add is a real
	// change rather than a duplicate-address error.
	_, _ = runNetsh("delete", "address", "name="+loopbackAdapter, "address=10.99.99.99")

	out, err := runNetsh("add", "address", "name="+loopbackAdapter,
		"address=10.99.99.99", "mask=255.255.255.255")
	if err != nil {
		if isElevationFailure(out) {
			return fmt.Errorf("%w: netsh needs an elevated shell (%s)",
				errUnsupported, strings.TrimSpace(out))
		}
		return fmt.Errorf("netsh add address: %w (%s)", err, out)
	}

	t.Cleanup(func() {
		_, _ = runNetsh("delete", "address", "name="+loopbackAdapter, "address=10.99.99.99")
	})
	return nil
}

func runNetsh(args ...string) (string, error) {
	full := append([]string{"interface", "ipv4"}, args...)
	// #nosec G204 -- every argument is a literal defined in this file
	out, err := exec.Command("netsh", full...).CombinedOutput()
	return string(out), err
}

func isElevationFailure(out string) bool {
	lowered := strings.ToLower(out)
	return strings.Contains(lowered, "requires elevation") ||
		strings.Contains(lowered, "access is denied") ||
		strings.Contains(lowered, "administrator")
}

// triggerScreenOff is deliberately not attempted. Blanking or locking the
// session on a hosted runner would cut off the very session the job runs in.
func triggerScreenOff(_ *testing.T) error {
	return fmt.Errorf("%w: locking or blanking the runner session would break the runner",
		errUnsupported)
}

// triggerInputActivity synthesises real pointer movement through SendInput,
// which advances the tick GetLastInputInfo reports to the sensor. Whether a
// hosted runner's window station permits this is measured, not assumed.
func triggerInputActivity(t *testing.T) error {
	t.Helper()

	user32 := syscall.NewLazyDLL("user32.dll")
	sendInput := user32.NewProc("SendInput")

	// Spread the movement over several seconds: the sensor polls once a second
	// and compares against its previous reading, so a single instantaneous
	// burst is easy to miss.
	for i := range 12 {
		in := newMouseMove(int32(10-(i%2)*20), int32(10-(i%2)*20))
		ret, _, err := sendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
		if ret == 0 {
			return fmt.Errorf("%w: SendInput was rejected by this window station (%v)",
				errUnsupported, err)
		}
		time.Sleep(400 * time.Millisecond)
	}
	return nil
}

// mouseInput mirrors the MOUSEINPUT structure from the Windows API.
type mouseInput struct {
	dx, dy      int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// input mirrors INPUT. The union is sized for its largest member, MOUSEINPUT,
// which is what this file always sends.
type input struct {
	inputType uint32
	_         uint32 // padding to the 8-byte alignment of the union on amd64
	mi        mouseInput
}

func newMouseMove(dx, dy int32) input {
	const (
		inputMouse      = 0
		mouseEventFMove = 0x0001
	)
	return input{
		inputType: inputMouse,
		mi:        mouseInput{dx: dx, dy: dy, dwFlags: mouseEventFMove},
	}
}

// unproducibleSensors names what no Windows runner can do, with the reason.
func unproducibleSensors() map[string]string {
	return map[string]string{
		"power": "the runner has no battery, so the charger can never be unplugged",
		"lid":   "the runner has no laptop lid and no MSAcpi_LidStatus WMI class",
		"usb":   "the runner has no attachable USB device to add or remove",
	}
}
