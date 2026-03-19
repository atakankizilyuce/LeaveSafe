//go:build windows

package alarm

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	winmm        = syscall.NewLazyDLL("winmm.dll")
	procPlaySound = winmm.NewProc("PlaySoundW")
)

const (
	sndMemory = 0x00000004
	sndSync   = 0x00000000
	sndPurge  = 0x00000040
)

// playWAV plays an in-memory WAV buffer synchronously using winmm.dll.
// It blocks until playback finishes or stopCh is closed.
func playWAV(wavData []byte, stopCh <-chan struct{}) error {
	done := make(chan struct{})

	go func() {
		defer close(done)
		procPlaySound.Call(
			uintptr(unsafe.Pointer(&wavData[0])),
			0,
			sndMemory|sndSync,
		)
	}()

	select {
	case <-done:
		return nil
	case <-stopCh:
		// Stop playback by purging
		stopPlayback()
		<-done
		return nil
	}
}

// stopPlayback stops any currently playing sound.
func stopPlayback() {
	procPlaySound.Call(0, 0, sndPurge)
	// Small delay to let the audio subsystem settle
	time.Sleep(50 * time.Millisecond)
}
