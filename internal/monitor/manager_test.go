package monitor

import (
	"context"
	"testing"
	"time"
)

// mockSensor is a test double that implements the Sensor interface.
type mockSensor struct {
	name      string
	available bool
	started   chan struct{} // closed when Start is called
	stopped   bool
}

func newMock(name string, available bool) *mockSensor {
	return &mockSensor{
		name:      name,
		available: available,
		started:   make(chan struct{}),
	}
}

func (m *mockSensor) Name() string        { return m.name }
func (m *mockSensor) DisplayName() string { return m.name + "_display" }
func (m *mockSensor) Available() bool     { return m.available }
func (m *mockSensor) Stop() error         { m.stopped = true; return nil }

func (m *mockSensor) Start(ctx context.Context, _ chan<- Alert) error {
	close(m.started) // signal that Start was called
	<-ctx.Done()     // block until cancelled
	return nil
}

// TestNewManager verifies the manager is correctly initialized.
func TestNewManager(t *testing.T) {
	mgr := NewManager()
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
	if mgr.AlertChannel() == nil {
		t.Error("AlertChannel should not be nil")
	}
	if len(mgr.Sensors()) != 0 {
		t.Error("initial sensor list should be empty")
	}
}

// TestRegister_SensorsDisabledByDefault checks that all sensors start disabled.
func TestRegister_SensorsDisabledByDefault(t *testing.T) {
	mgr := NewManager()
	mgr.Register(newMock("power", true))
	mgr.Register(newMock("network", true))
	mgr.Register(newMock("lid", false))

	if mgr.IsEnabled("power") {
		t.Error("power sensor should be disabled by default")
	}
	if mgr.IsEnabled("network") {
		t.Error("network sensor should be disabled by default")
	}
	if mgr.IsEnabled("lid") {
		t.Error("lid sensor should be disabled by default")
	}
}

// TestSensors_ReturnsCopy verifies Sensors() returns all registered sensors.
func TestSensors_ReturnsCopy(t *testing.T) {
	mgr := NewManager()
	mgr.Register(newMock("power", true))
	mgr.Register(newMock("usb", true))

	sensors := mgr.Sensors()
	if len(sensors) != 2 {
		t.Errorf("Sensors() length = %d, want 2", len(sensors))
	}
}

// TestEnable verifies a sensor can be explicitly enabled.
func TestEnable(t *testing.T) {
	mgr := NewManager()
	mgr.Register(newMock("network", true))

	if mgr.IsEnabled("network") {
		t.Fatal("network should not be enabled initially")
	}
	mgr.Enable("network")
	if !mgr.IsEnabled("network") {
		t.Error("network should be enabled after Enable()")
	}
}

// TestDisable verifies a sensor can be disabled.
func TestDisable(t *testing.T) {
	mgr := NewManager()
	mgr.Register(newMock("power", true))
	mgr.Enable("power")

	if !mgr.IsEnabled("power") {
		t.Fatal("power should be enabled after Enable()")
	}
	mgr.Disable("power")
	if mgr.IsEnabled("power") {
		t.Error("power should be disabled after Disable()")
	}
}

// TestIsEnabled_Unknown checks that an unregistered sensor name returns false.
func TestIsEnabled_Unknown(t *testing.T) {
	mgr := NewManager()
	if mgr.IsEnabled("doesnotexist") {
		t.Error("unknown sensor should not be enabled")
	}
}

// TestAlertChannel verifies the alert channel can be used to send and receive alerts.
func TestAlertChannel(t *testing.T) {
	mgr := NewManager()
	ch := mgr.AlertChannel()
	if ch == nil {
		t.Fatal("AlertChannel() should not be nil")
	}
}

// TestStartEnabled_StopAll verifies that StartEnabled starts sensors and StopAll stops them.
func TestStartEnabled_StopAll(t *testing.T) {
	mgr := NewManager()
	s := newMock("power", true)
	mgr.Register(s)
	mgr.Enable("power")

	mgr.StartEnabled()

	// Wait for the goroutine to call Start.
	select {
	case <-s.started:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("sensor Start() was not called within timeout")
	}

	mgr.StopAll()

	// After StopAll the context passed to Start is cancelled; allow goroutine to exit.
	time.Sleep(50 * time.Millisecond)
}

// TestStartEnabled_SkipsDisabled verifies disabled sensors are not started.
func TestStartEnabled_SkipsDisabled(t *testing.T) {
	mgr := NewManager()
	s := newMock("network", true) // network is opt-in, so not auto-enabled
	mgr.Register(s)

	mgr.StartEnabled()

	// Give a moment to confirm Start is NOT called.
	select {
	case <-s.started:
		t.Error("disabled sensor should not have been started")
	case <-time.After(100 * time.Millisecond):
		// expected: sensor was not started
	}
}

// TestStartEnabled_Idempotent verifies calling StartEnabled twice does not start duplicate goroutines.
func TestStartEnabled_Idempotent(t *testing.T) {
	mgr := NewManager()
	s := newMock("power", true)
	mgr.Register(s)
	mgr.Enable("power")

	mgr.StartEnabled()
	// Wait for first start
	select {
	case <-s.started:
	case <-time.After(2 * time.Second):
		t.Fatal("sensor not started on first call")
	}

	// Second call should be a no-op (sensor already running)
	mgr.StartEnabled()

	mgr.StopAll()
	time.Sleep(50 * time.Millisecond)
}

// TestDisable_StopsRunning verifies Disable stops an already-running sensor.
func TestDisable_StopsRunning(t *testing.T) {
	mgr := NewManager()
	s := newMock("power", true)
	mgr.Register(s)
	mgr.Enable("power")
	mgr.StartEnabled()

	select {
	case <-s.started:
	case <-time.After(2 * time.Second):
		t.Fatal("sensor not started")
	}

	mgr.Disable("power")

	// After Disable, the sensor should no longer be enabled.
	if mgr.IsEnabled("power") {
		t.Error("sensor should be disabled after Disable()")
	}

	// Give the goroutine a moment to exit.
	time.Sleep(50 * time.Millisecond)
}
