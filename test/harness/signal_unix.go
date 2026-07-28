//go:build !windows

package harness

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup is a no-op on Unix; the default group is fine.
func configureProcessGroup(_ *exec.Cmd) {}

// terminate asks the process to shut down the way a real operator would.
func terminate(cmd *exec.Cmd) error {
	return cmd.Process.Signal(syscall.SIGTERM)
}
