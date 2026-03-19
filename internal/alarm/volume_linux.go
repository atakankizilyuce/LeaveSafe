//go:build linux

package alarm

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// maxVolume saves the current volume level and sets it to 100%.
// Returns the previous volume level (0.0 - 1.0 range).
func maxVolume() (float64, error) {
	var prev float64

	// Try PulseAudio first
	if _, err := exec.LookPath("pactl"); err == nil {
		prev = getPulseVolume()
		if err := exec.Command("pactl", "set-sink-mute", "@DEFAULT_SINK@", "0").Run(); err == nil {
			if err := exec.Command("pactl", "set-sink-volume", "@DEFAULT_SINK@", "100%").Run(); err == nil {
				return prev, nil
			}
		}
	}

	// Fall back to ALSA
	if _, err := exec.LookPath("amixer"); err == nil {
		prev = getAlsaVolume()
		if err := exec.Command("amixer", "sset", "Master", "100%", "unmute").Run(); err != nil {
			return prev, fmt.Errorf("amixer: %w", err)
		}
		return prev, nil
	}

	return 0, fmt.Errorf("no audio control tool found (pactl or amixer)")
}

func getPulseVolume() float64 {
	out, err := exec.Command("pactl", "get-sink-volume", "@DEFAULT_SINK@").Output()
	if err != nil {
		return 0
	}
	// Output format: "Volume: front-left: 65536 / 100% / 0.00 dB, ..."
	s := string(out)
	if idx := strings.Index(s, "/ "); idx >= 0 {
		s = s[idx+2:]
		if end := strings.Index(s, "%"); end >= 0 {
			v, _ := strconv.Atoi(s[:end])
			return float64(v) / 100.0
		}
	}
	return 0
}

func getAlsaVolume() float64 {
	out, err := exec.Command("amixer", "sget", "Master").Output()
	if err != nil {
		return 0
	}
	// Output format: "... [75%] ..."
	s := string(out)
	if idx := strings.Index(s, "["); idx >= 0 {
		s = s[idx+1:]
		if end := strings.Index(s, "%]"); end >= 0 {
			v, _ := strconv.Atoi(s[:end])
			return float64(v) / 100.0
		}
	}
	return 0
}

// restoreVolume sets the system volume back to the saved level.
func restoreVolume(level float64) error {
	vol := int(level * 100)

	if _, err := exec.LookPath("pactl"); err == nil {
		return exec.Command("pactl", "set-sink-volume", "@DEFAULT_SINK@", fmt.Sprintf("%d%%", vol)).Run()
	}
	if _, err := exec.LookPath("amixer"); err == nil {
		return exec.Command("amixer", "sset", "Master", fmt.Sprintf("%d%%", vol)).Run()
	}
	return fmt.Errorf("no audio control tool found")
}
