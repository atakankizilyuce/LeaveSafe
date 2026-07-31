//go:build !windows

package syspath

// System32 returns name unchanged. There is no System32 here, and the callers
// that use it are all behind Windows build tags; this exists so the package
// compiles and lints on every target rather than only on one.
func System32(name string) string { return name }

// PowerShell returns the bare name, for the same reason.
func PowerShell() string { return "powershell" }
