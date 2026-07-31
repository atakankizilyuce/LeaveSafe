//go:build windows

package syspath

import (
	"os"
	"path/filepath"
)

// windowsDir returns the Windows directory, falling back to the conventional
// location when the environment does not say.
func windowsDir() string {
	for _, name := range []string{"SystemRoot", "windir"} {
		if dir := os.Getenv(name); dir != "" {
			return dir
		}
	}
	return `C:\Windows`
}

// resolve returns the absolute path to a file below the Windows directory, or
// its bare name if it is not there.
//
// The fallback matters: an install that keeps its system files somewhere this
// does not predict should lose the hardening, not the sensor. A monitor that
// stops watching is a worse outcome than one that resolves a name the way it
// always did.
func resolve(rel string) string {
	full := filepath.Join(windowsDir(), rel)
	if info, err := os.Stat(full); err == nil && !info.IsDir() {
		return full
	}
	return filepath.Base(rel)
}

// System32 returns the absolute path to name in System32.
func System32(name string) string {
	return resolve(filepath.Join("System32", name))
}

// PowerShell returns the absolute path to Windows PowerShell.
func PowerShell() string {
	return resolve(filepath.Join("System32", "WindowsPowerShell", "v1.0", "powershell.exe"))
}
