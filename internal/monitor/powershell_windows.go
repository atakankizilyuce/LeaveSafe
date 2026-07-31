//go:build windows

package monitor

import (
	"context"
	"os/exec"
	"time"

	"github.com/leavesafe/leavesafe/internal/syspath"
)

// The two kinds of question this package asks PowerShell, and how long each is
// worth waiting for. They differ because a wrong answer costs something
// different in each case.
const (
	// availabilityTimeout bounds "can this sensor work on this machine at all".
	// It is generous: the question is asked once per run, and answering it
	// wrongly is worse than answering it slowly — reporting a real laptop's lid
	// as unavailable quietly removes a sensor the owner is relying on. Starting
	// a PowerShell process at all can take seconds on a cold or loaded machine,
	// before WMI has been asked anything.
	availabilityTimeout = 20 * time.Second

	// pollTimeout bounds a reading taken while armed, every couple of seconds.
	// A late reading is worthless because the next one is already due, so this
	// stays tight — and a sensor that cannot answer in four seconds is not
	// going to catch a lid closing.
	pollTimeout = 4 * time.Second
)

// powershellOutput runs a PowerShell one-liner and returns its standard output.
//
// -NoProfile skips the user's profile script, which is somebody else's code and
// somebody else's startup cost on a call this program makes every couple of
// seconds. -NonInteractive means a cmdlet that wants to prompt fails instead of
// waiting forever for an answer nobody is there to give. The USB sensor has
// always run its script this way; the rest now do too.
func powershellOutput(ctx context.Context, timeout time.Duration, script string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The interpreter is named by absolute path rather than left to PATH. The
	// script has always been a constant, so the arguments were never the risk;
	// which binary answers to "powershell" was, and PATH is not this program's
	// to trust on a call it makes every couple of seconds while armed.
	//
	// #nosec G204 -- script is a constant defined in this package, never user input
	return exec.CommandContext(ctx,
		syspath.PowerShell(), "-NoProfile", "-NonInteractive", "-Command", script).Output()
}
