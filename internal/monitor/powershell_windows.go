//go:build windows

package monitor

import (
	"context"
	"os/exec"
	"time"
)

// probeTimeout bounds a single PowerShell probe.
//
// These queries are cheap on a real laptop and occasionally are not. WMI in
// particular can stall for tens of seconds on a machine with no battery or no
// ACPI lid — a virtual machine, a desktop, a CI runner — and an unbounded probe
// is not merely slow. Availability is checked while the sensors are registered,
// which happens before the server binds, so one hanging query delays the moment
// the QR code starts working. Five seconds is far longer than a healthy answer
// takes and short enough that a wedged one is not mistaken for a working alarm.
const probeTimeout = 5 * time.Second

// powershellOutput runs a PowerShell one-liner and returns its standard output.
//
// -NoProfile skips the user's profile script, which is somebody else's code and
// somebody else's startup cost on a call this program makes every couple of
// seconds. -NonInteractive means a cmdlet that wants to prompt fails instead of
// waiting forever for an answer nobody is there to give. The USB sensor has
// always run its script this way; the rest now do too.
func powershellOutput(ctx context.Context, script string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	// #nosec G204 -- script is a constant defined in this package, never user input
	return exec.CommandContext(ctx,
		"powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
}
