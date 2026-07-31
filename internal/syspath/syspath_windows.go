//go:build windows

package syspath

import (
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows"
)

// system32 is the real System32 directory, asked of the kernel once and kept.
//
// It used to be read from %SystemRoot% or %windir%, which handed the answer
// back to whatever launched this process — and HKCU\Environment is writable by
// an ordinary user, so those variables are the user's to set. Pointing
// SystemRoot at a directory they own let them choose which powershell.exe the
// monitor runs every few seconds while armed, and which schtasks.exe
// install-service runs from the elevated prompt the documentation asks for.
// That is the PATH hijack this package exists to close, arriving by a different
// door. The environment is not a trust boundary; GetSystemDirectory asks the
// kernel, which is.
//
// The literal is only reached if the call itself fails, which on a running
// Windows it does not.
var system32 = sync.OnceValue(func() string {
	if dir, err := windows.GetSystemDirectory(); err == nil && dir != "" {
		return dir
	}
	return `C:\Windows\System32`
})

// resolve returns the absolute path to a file below System32.
//
// It does not check that the file is there, and deliberately returns the
// absolute path either way. The previous version fell back to the bare name
// when the file was missing, which handed the lookup straight back to PATH —
// so any machine whose system directory this did not predict lost the whole
// defense silently. A sensor that fails to start says so, on the dashboard and
// to the phone; one that quietly runs a stranger's binary does not.
func resolve(rel ...string) string {
	return filepath.Join(append([]string{system32()}, rel...)...)
}

// System32 returns the absolute path to name in System32.
func System32(name string) string {
	return resolve(name)
}

// PowerShell returns the absolute path to Windows PowerShell.
func PowerShell() string {
	return resolve("WindowsPowerShell", "v1.0", "powershell.exe")
}
