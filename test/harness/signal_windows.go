//go:build windows

package harness

import (
	"fmt"
	"os/exec"
	"syscall"
)

// createNewProcessGroup is CREATE_NEW_PROCESS_GROUP from the Windows API.
const createNewProcessGroup = 0x00000200

// configureProcessGroup puts the child in its own console process group so a
// CTRL+BREAK can be addressed to it without hitting the test runner too.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// terminate sends CTRL+BREAK, which the Go runtime surfaces in the child as
// os.Interrupt — the signal main.go installs a handler for. Windows has no
// SIGTERM, so this is the closest equivalent to what an operator would do.
func terminate(cmd *exec.Cmd) error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GenerateConsoleCtrlEvent")
	const ctrlBreakEvent = 1
	ret, _, err := proc.Call(uintptr(ctrlBreakEvent), uintptr(cmd.Process.Pid))
	if ret == 0 {
		return fmt.Errorf("GenerateConsoleCtrlEvent: %w", err)
	}
	return nil
}
