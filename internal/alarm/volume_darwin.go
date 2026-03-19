//go:build darwin

package alarm

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// maxVolume saves the current volume level and sets it to 100%.
// Returns the previous volume level (0.0 - 1.0 range, mapped from 0-100).
func maxVolume() (float64, error) {
	// Get current volume (0-100)
	out, err := exec.Command("osascript", "-e", "output volume of (get volume settings)").Output()
	var prev float64
	if err == nil {
		v, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		prev = float64(v) / 100.0
	}

	// Set volume to max and unmute
	if err := exec.Command("osascript", "-e", "set volume output volume 100").Run(); err != nil {
		return prev, fmt.Errorf("set volume: %w", err)
	}
	exec.Command("osascript", "-e", "set volume without output muted").Run()

	return prev, nil
}

// restoreVolume sets the system volume back to the saved level.
func restoreVolume(level float64) error {
	vol := int(level * 100)
	return exec.Command("osascript", "-e", fmt.Sprintf("set volume output volume %d", vol)).Run()
}
