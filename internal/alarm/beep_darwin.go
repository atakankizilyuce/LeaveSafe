//go:build darwin

package alarm

import (
	"fmt"
	"time"
)

func beepTone(freq int, durationMs int, stopCh <-chan struct{}) {
	fmt.Print("\a")

	timer := time.NewTimer(time.Duration(durationMs) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-stopCh:
	}
}
