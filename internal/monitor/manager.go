package monitor

import (
	"context"
	"sync"

	log "github.com/sirupsen/logrus"
)

// Manager handles sensor registration, lifecycle, and alert routing.
type Manager struct {
	mu       sync.RWMutex
	sensors  []Sensor
	enabled  map[string]bool
	cancels  map[string]context.CancelFunc
	alertCh  chan Alert
}

// NewManager creates a new sensor manager.
func NewManager() *Manager {
	return &Manager{
		enabled: make(map[string]bool),
		cancels: make(map[string]context.CancelFunc),
		alertCh: make(chan Alert, 100),
	}
}

// Register adds a sensor to the manager. Sensors start disabled by default.
func (m *Manager) Register(s Sensor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sensors = append(m.sensors, s)
}

// Sensors returns all registered sensors.
func (m *Manager) Sensors() []Sensor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Sensor, len(m.sensors))
	copy(result, m.sensors)
	return result
}

// AlertChannel returns the channel where alerts are sent.
func (m *Manager) AlertChannel() <-chan Alert {
	return m.alertCh
}

// IsEnabled checks if a sensor is enabled.
func (m *Manager) IsEnabled(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled[name]
}

// Enable enables a sensor.
func (m *Manager) Enable(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled[name] = true
}

// Disable disables a sensor and stops it if running.
func (m *Manager) Disable(name string) {
	m.mu.Lock()
	m.enabled[name] = false
	cancel, exists := m.cancels[name]
	if exists {
		cancel()
		delete(m.cancels, name)
	}
	m.mu.Unlock()
}

// StartEnabled starts all enabled and available sensors.
func (m *Manager) StartEnabled() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.sensors {
		if !m.enabled[s.Name()] || !s.Available() {
			continue
		}
		// Don't start if already running
		if _, running := m.cancels[s.Name()]; running {
			continue
		}

		ctx, cancel := context.WithCancel(context.Background())
		m.cancels[s.Name()] = cancel
		alertCh := m.alertCh

		go func(sensor Sensor) {
			log.WithField("sensor", sensor.Name()).Info("Sensor started")
			if err := sensor.Start(ctx, alertCh); err != nil {
				if ctx.Err() == nil {
					log.WithField("sensor", sensor.Name()).Errorf("Sensor error: %v", err)
				}
			}
			log.WithField("sensor", sensor.Name()).Info("Sensor stopped")
		}(s)
	}
}

// StopAll stops all running sensors.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, cancel := range m.cancels {
		cancel()
		delete(m.cancels, name)
	}

	for _, s := range m.sensors {
		_ = s.Stop()
	}
}
