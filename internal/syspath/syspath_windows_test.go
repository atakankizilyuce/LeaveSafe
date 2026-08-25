//go:build windows

package syspath

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// The whole point of this package is that the answer does not come from
// anywhere the process being defended can be told to look. %SystemRoot% is
// writable by an ordinary user through HKCU\Environment, so honoring it meant
// they got to choose which powershell.exe the monitor runs every few seconds
// while armed — and which schtasks.exe install-service runs from the elevated
// prompt the documentation asks for.
//
// The decoys have to actually exist, because the trap is that a planted file is
// found and returned. An empty directory would let this pass against the very
// code it is here to catch.
func TestTheSystemDirectoryIgnoresTheEnvironment(t *testing.T) {
	planted := t.TempDir()
	plantedSystem32 := filepath.Join(planted, "System32")
	plantedPowerShell := filepath.Join(plantedSystem32, "WindowsPowerShell", "v1.0")
	for _, dir := range []string{plantedSystem32, plantedPowerShell} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("plant %s: %v", dir, err)
		}
	}
	for _, decoy := range []string{
		filepath.Join(plantedSystem32, "netsh.exe"),
		filepath.Join(plantedSystem32, "schtasks.exe"),
		filepath.Join(plantedPowerShell, "powershell.exe"),
	} {
		if err := os.WriteFile(decoy, []byte("MZ"), 0o700); err != nil {
			t.Fatalf("plant %s: %v", decoy, err)
		}
	}

	t.Setenv("SystemRoot", planted)
	t.Setenv("windir", planted)

	for name, got := range map[string]string{
		"netsh":      System32("netsh.exe"),
		"schtasks":   System32("schtasks.exe"),
		"powershell": PowerShell(),
	} {
		if strings.HasPrefix(strings.ToLower(got), strings.ToLower(planted)) {
			t.Errorf("%s resolved to %q — a binary an ordinary user planted, chosen by an "+
				"environment variable that same user can set", name, got)
		}
	}
}

// A bare name is a PATH lookup, which is the thing being defended against. The
// resolved path has to stay absolute even when nothing is there to run.
func TestEveryResolvedPathIsAbsolute(t *testing.T) {
	for name, got := range map[string]string{
		"System32":   System32("schtasks.exe"),
		"PowerShell": PowerShell(),
		"missing":    System32("no-such-tool-should-exist.exe"),
	} {
		if !filepath.IsAbs(got) {
			t.Errorf("%s resolved to %q, which PATH would get to answer", name, got)
		}
	}
}

// And the paths must actually name the tools, or the hardening would be a way
// to stop the monitor rather than to protect it.
func TestTheResolvedToolsAreReallyThere(t *testing.T) {
	for name, path := range map[string]string{
		"netsh":      System32("netsh.exe"),
		"schtasks":   System32("schtasks.exe"),
		"powershell": PowerShell(),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s resolved to %q, which is not there: %v", name, path, err)
		}
	}
}

// A helper that writes its answer down a pipe should never be seen. Without
// this flag it is seen whenever there is no console for it to inherit — which
// is the autostart entry, where the user is sitting at their desktop using
// something else while a window opens in front of it every couple of seconds.
func TestAHelperIsGivenNoWindowOfItsOwn(t *testing.T) {
	cmd := exec.Command(System32("netsh.exe"), "wlan", "show", "networks")

	HideWindow(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("the child was left to whatever Windows does by default")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Errorf("creation flags are %#x, which still lets Windows open a window",
			cmd.SysProcAttr.CreationFlags)
	}
}

// The flags a caller already set are kept. Nothing sets any today, and a helper
// that quietly dropped them would be a trap for whatever does first.
func TestHidingAWindowKeepsTheFlagsAlreadyAskedFor(t *testing.T) {
	cmd := exec.Command(System32("netsh.exe"))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}

	HideWindow(cmd)

	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Error("a flag the caller asked for was dropped")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Error("the window flag was not added")
	}
}
