//go:build sandbox && linux

// Package linuxvm creates real kernel-backed hardware inside a VM so the
// unmodified binary reads a real /sys and /dev. Nothing here fakes a sensor
// reading: every helper produces a change the kernel itself reports, through
// exactly the files the production code opens.
//
// It is meant to run as root inside the throwaway VM that run.sh boots. Running
// it on a workstation would load modules into that machine's kernel, so every
// scenario checks for the VM marker first.
package linuxvm

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// errNoHardware marks hardware the running kernel cannot provide. It is a
// reason to skip a scenario and record the gap, never a reason to fake it.
var errNoHardware = errors.New("hardware unavailable in this environment")

// sandboxMarker is written by cloud-init. Its absence means we are not inside
// the throwaway VM, and loading kernel modules would be somebody's workstation.
const sandboxMarker = "/etc/leavesafe-sandbox"

// requireSandboxVM refuses to touch the kernel outside the disposable VM.
func requireSandboxVM() error {
	if _, err := os.Stat(sandboxMarker); err != nil {
		return fmt.Errorf("%w: not running inside the sandbox VM (%s is absent); "+
			"start it with test/sandbox/linuxvm/run.sh", errNoHardware, sandboxMarker)
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("%w: creating kernel-backed hardware needs root", errNoHardware)
	}
	return nil
}

// loadModule inserts a kernel module, tolerating one that is already loaded.
func loadModule(name string) error {
	if err := requireSandboxVM(); err != nil {
		return err
	}
	// #nosec G204 -- name comes from a fixed set of literals in the scenarios
	if out, err := exec.Command("modprobe", name).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: modprobe %s: %v (%s)",
			errNoHardware, name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// setTestPowerAC drives the synthetic AC adapter the test_power module exposes.
// The kernel republishes the new state through /sys/class/power_supply, which is
// exactly the file the production sensor reads — so unplugging the charger here
// is a real kernel event, not a stubbed return value.
func setTestPowerAC(online bool) error {
	value := "0"
	if online {
		value = "1"
	}
	const param = "/sys/module/test_power/parameters/ac_online"
	if err := os.WriteFile(param, []byte(value), 0o600); err != nil {
		return fmt.Errorf("%w: write %s: %v", errNoHardware, param, err)
	}
	// Give the kernel a moment to republish through sysfs.
	time.Sleep(500 * time.Millisecond)

	matches, err := filepath.Glob("/sys/class/power_supply/*/online")
	if err != nil || len(matches) == 0 {
		return fmt.Errorf("%w: test_power exposed no power supply under /sys", errNoHardware)
	}
	return nil
}

// virtualKeyboard is a uinput device that emits real key events.
type virtualKeyboard struct {
	devicePath string
}

// createVirtualKeyboard finds the input device uinput created and returns a
// handle that can type on it.
func createVirtualKeyboard() (*virtualKeyboard, error) {
	if err := requireSandboxVM(); err != nil {
		return nil, err
	}
	if _, err := os.Stat("/dev/uinput"); err != nil {
		return nil, fmt.Errorf("%w: /dev/uinput is absent: %v", errNoHardware, err)
	}
	if _, err := exec.LookPath("evemu-device"); err != nil {
		return nil, fmt.Errorf("%w: evemu-tools is not installed: %v", errNoHardware, err)
	}

	matches, err := filepath.Glob("/dev/input/event*")
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("%w: no /dev/input/event* device exists", errNoHardware)
	}
	return &virtualKeyboard{devicePath: matches[0]}, nil
}

// Type emits real key-down/key-up pairs, spread out so the sensor — which polls
// once a second and needs consecutive changes — actually observes them.
func (k *virtualKeyboard) Type(rounds int) {
	for range rounds {
		// #nosec G204 -- devicePath comes from a glob of /dev/input
		_ = exec.Command("evemu-event", k.devicePath,
			"--type", "EV_KEY", "--code", "KEY_A", "--value", "1", "--sync").Run()
		// #nosec G204 -- devicePath comes from a glob of /dev/input
		_ = exec.Command("evemu-event", k.devicePath,
			"--type", "EV_KEY", "--code", "KEY_A", "--value", "0", "--sync").Run()
		time.Sleep(400 * time.Millisecond)
	}
}

// attachDummyUSB binds a gadget to the dummy_hcd virtual host controller, which
// makes a real device node appear under /sys/bus/usb/devices.
func attachDummyUSB() error {
	if err := loadModule("g_zero"); err != nil {
		return fmt.Errorf("%w: no USB gadget could be bound: %v", errNoHardware, err)
	}
	time.Sleep(2 * time.Second)

	entries, err := os.ReadDir("/sys/bus/usb/devices")
	if err != nil {
		return fmt.Errorf("%w: /sys/bus/usb/devices is absent: %v", errNoHardware, err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("%w: no USB device appeared after binding the gadget", errNoHardware)
	}
	return nil
}

// xServer is a real X server the screen sensor can query.
type xServer struct {
	cmd *exec.Cmd
}

// startXvfb brings up a real X server with the DPMS extension enabled. Without
// `+extension DPMS`, Xvfb reports "Server does not have the DPMS Extension" and
// `xset dpms force off` cannot change anything — which is exactly what the
// captured runner fixture shows.
func startXvfb() (*xServer, error) {
	if _, err := exec.LookPath("Xvfb"); err != nil {
		return nil, fmt.Errorf("%w: Xvfb is not installed: %v", errNoHardware, err)
	}

	cmd := exec.Command("Xvfb", ":99", "-screen", "0", "1024x768x24", "+extension", "DPMS")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Xvfb: %w", err)
	}
	srv := &xServer{cmd: cmd}

	if err := os.Setenv("DISPLAY", ":99"); err != nil {
		srv.Stop()
		return nil, fmt.Errorf("set DISPLAY: %w", err)
	}
	time.Sleep(2 * time.Second)

	out, err := exec.Command("xset", "q").CombinedOutput()
	if err != nil {
		srv.Stop()
		return nil, fmt.Errorf("%w: xset cannot reach the display: %v (%s)",
			errNoHardware, err, strings.TrimSpace(string(out)))
	}
	if strings.Contains(string(out), "does not have the DPMS Extension") {
		srv.Stop()
		return nil, fmt.Errorf("%w: this Xvfb build has no DPMS extension, "+
			"so the screen can never report itself off", errNoHardware)
	}
	return srv, nil
}

// Stop tears the X server down.
func (s *xServer) Stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
}

// forceDPMSOff blanks the real X display, which is what the sensor detects.
func forceDPMSOff() error {
	if out, err := exec.Command("xset", "dpms", "force", "off").CombinedOutput(); err != nil {
		return fmt.Errorf("xset dpms force off: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// addLoopbackAddress changes the machine's real IP set.
func addLoopbackAddress() error {
	if out, err := exec.Command("ip", "addr", "add", "10.99.99.99/32", "dev", "lo").CombinedOutput(); err != nil {
		return fmt.Errorf("ip addr add: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
