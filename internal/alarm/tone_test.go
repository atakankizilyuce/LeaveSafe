package alarm

import (
	"github.com/leavesafe/leavesafe/internal/config"
	"testing"
)

func TestAlarmReadsSelectedToneForEveryNewPlayback(t *testing.T) {
	oldTone, oldSiren := playToneFn, playSirenFn
	t.Cleanup(func() { playToneFn, playSirenFn = oldTone, oldSiren })
	selected := "bells"
	played := ""
	playToneFn = func(tone string, _ <-chan struct{}) error { played = tone; return nil }
	playSirenFn = func(_ <-chan struct{}) error { played = "original"; return nil }
	a := NewWithToneSource(config.AlarmConfig{}, func() string { return selected })
	for _, tone := range []string{"bells", "beep", "original", ""} {
		selected = tone
		a.sirenLoop(make(chan struct{}))
		want := tone
		if want == "" {
			want = "original"
		}
		if played != want {
			t.Fatalf("played %s, want %s", played, want)
		}
	}
}
