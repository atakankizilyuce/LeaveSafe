//go:build windows

package alarm

import "syscall"

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	procBeep = kernel32.NewProc("Beep")
)

func beepTone(freq int, durationMs int, stopCh <-chan struct{}) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		procBeep.Call(uintptr(freq), uintptr(durationMs))
	}()

	select {
	case <-done:
	case <-stopCh:
		<-done
	}
}
