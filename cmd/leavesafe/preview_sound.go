package main

import (
	"github.com/leavesafe/leavesafe/internal/audio"
	"time"
)

// Preview runs without starting monitoring, touching settings or raising system
// volume. It is bounded even if the parent application exits unexpectedly.
func previewAlarmSound(args []string) int {
	if len(args) != 1 || !audio.ValidTone(args[0]) {
		return 2
	}
	stop := make(chan struct{})
	timer := time.AfterFunc(3*time.Second, func() { close(stop) })
	defer timer.Stop()
	if err := audio.PlayTone(args[0], stop); err != nil {
		return 1
	}
	return 0
}
