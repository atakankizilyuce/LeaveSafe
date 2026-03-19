//go:build darwin

package alarm

import (
	"os"
	"os/exec"
	"sync"
	"time"
)

var (
	playerMu  sync.Mutex
	playerCmd *exec.Cmd
	tempFile  string
)

// playWAV writes WAV data to a temp file and plays it with afplay.
// It blocks until playback finishes or stopCh is closed.
func playWAV(wavData []byte, stopCh <-chan struct{}) error {
	playerMu.Lock()
	if tempFile == "" {
		f, err := os.CreateTemp("", "leavesafe-alarm-*.wav")
		if err != nil {
			playerMu.Unlock()
			return err
		}
		f.Write(wavData)
		f.Close()
		tempFile = f.Name()
	}

	cmd := exec.Command("afplay", tempFile)
	playerCmd = cmd
	playerMu.Unlock()

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-stopCh:
		cmd.Process.Kill()
		<-done
		return nil
	}
}

// stopPlayback kills any running afplay process and cleans up.
func stopPlayback() {
	playerMu.Lock()
	defer playerMu.Unlock()

	if playerCmd != nil && playerCmd.Process != nil {
		playerCmd.Process.Kill()
		playerCmd = nil
	}
	if tempFile != "" {
		os.Remove(tempFile)
		tempFile = ""
	}
	time.Sleep(50 * time.Millisecond)
}
