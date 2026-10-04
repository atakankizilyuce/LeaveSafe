//go:build darwin

package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/ebitengine/purego"
)

var (
	mainDisplayID   func() uint32
	displayIsAsleep func(uint32) int32
)

func init() {
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY)
	if err != nil {
		return
	}
	main, err := purego.Dlsym(lib, "CGMainDisplayID")
	if err != nil {
		return
	}
	asleep, err := purego.Dlsym(lib, "CGDisplayIsAsleep")
	if err != nil {
		return
	}
	purego.RegisterFunc(&mainDisplayID, main)
	purego.RegisterFunc(&displayIsAsleep, asleep)
}

// ScreenSensor monitors the display/screen state on macOS.
type ScreenSensor struct {
	watch stateWatch[bool]

	// read is how the display is asked and every is how often. Both are filled
	// in by the constructor; a test replaces them to drive the loop without the
	// hardware, and without waiting two seconds for every reading.
	read  func(context.Context) (bool, error)
	every time.Duration
}

func NewScreenSensor() *ScreenSensor {
	return &ScreenSensor{
		read:  func(context.Context) (bool, error) { return isScreenOnDarwin() },
		every: 2 * time.Second,
	}
}

func (s *ScreenSensor) Name() string        { return "screen" }
func (s *ScreenSensor) DisplayName() string { return "Screen/Display" }
func (s *ScreenSensor) Available() bool     { return mainDisplayID != nil && displayIsAsleep != nil }

func (s *ScreenSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	return poll{
		every: s.every,
		read:  s.read,
		alert: screenAlert,
		watch: &s.watch,
	}.run(ctx, alerts)
}

func (s *ScreenSensor) Stop() error { return nil }

func isScreenOnDarwin() (bool, error) {
	return readDisplayOn(mainDisplayID, displayIsAsleep)
}

// Modern Apple Silicon no longer exposes DevicePowerState on
// IODisplayWrangler. Use Quartz instead of treating a missing field as "on".
func readDisplayOn(main func() uint32, asleep func(uint32) int32) (bool, error) {
	if main == nil || asleep == nil {
		return false, fmt.Errorf("CoreGraphics display services unavailable")
	}
	id := main()
	if id == 0 {
		return false, fmt.Errorf("no main display available")
	}
	state := asleep(id)
	if state < 0 {
		return false, fmt.Errorf("cannot read display sleep state")
	}
	return state == 0, nil
}
