//go:build !windows

package syspath

import "os/exec"

// System32 returns name unchanged. There is no System32 here, and the callers
// that use it are all behind Windows build tags; this exists so the package
// compiles and lints on every target rather than only on one.
func System32(name string) string { return name }

// PowerShell returns the bare name, for the same reason.
func PowerShell() string { return "powershell" }

// HideWindow does nothing away from Windows. Nothing here opens a window for a
// process that writes its answer down a pipe, so there is none to hide.
func HideWindow(*exec.Cmd) {
	// Empty on purpose: the emptiness is the whole implementation. Creating a
	// process with no window is a Windows creation flag with no equivalent on
	// any other platform, and nothing to stand in for it — a helper started
	// here is never given a window in the first place.
}
