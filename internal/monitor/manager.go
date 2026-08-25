package monitor

import (
	"context"
	"errors"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/safe"
)

// errStoppedWatching is the failure recorded for a sensor whose driver returned
// without being asked to and without saying why.
var errStoppedWatching = errors.New("the sensor stopped watching without being asked to")

// Manager handles sensor registration, lifecycle, and alert routing.
type Manager struct {
	mu      sync.RWMutex
	sensors []Sensor
	enabled map[string]bool
	cancels map[string]context.CancelFunc
	alertCh chan Alert

	// failures records why a sensor is not watching right now, keyed by name and
	// empty when it is. This is what stops the panel claiming a sensor is active
	// while its loop is between restarts or failing repeatedly — an alarm that
	// reports coverage it does not have is worse than one that reports none.
	failures map[string]string

	// availability holds the settled answer to "can this sensor work here",
	// and probing names the sensors still finding out. See availability.go for
	// why the answer is cached here rather than left to each sensor.
	availability map[string]bool
	probing      map[string]bool
}

// NewManager creates a new sensor manager.
func NewManager() *Manager {
	return &Manager{
		enabled:      make(map[string]bool),
		cancels:      make(map[string]context.CancelFunc),
		alertCh:      make(chan Alert, 100),
		failures:     make(map[string]string),
		availability: make(map[string]bool),
		probing:      make(map[string]bool),
	}
}

// recordFailure notes that a sensor is not watching, and why.
func (m *Manager) recordFailure(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures[name] = err.Error()
}

// clearFailure marks a sensor as watching again. Called at the top of every
// attempt, so a restart that succeeds clears the record the failure left.
func (m *Manager) clearFailure(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.failures, name)
}

// Failure returns why a sensor is not watching, or "" when it is.
func (m *Manager) Failure(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.failures[name]
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
	// A sensor switched off is not a sensor that failed. Leaving the record
	// behind would report a fault the user chose.
	delete(m.failures, name)
	m.mu.Unlock()
}

// StartEnabled starts all enabled and available sensors.
//
// Arming is the one caller allowed to wait for an availability answer: it is a
// deliberate act with a person behind it, and starting the right set of sensors
// is worth a pause in a way that answering a socket is not. But it waits
// *outside* the lock, and only for what is not already known — see
// availabilityFor. Asking under the lock meant a twenty-second WMI query on
// Windows held the manager's mutex for the duration, and asking every sensor
// from scratch, one after another, meant the goroutine that typed `arm` was
// gone for the sum of them.
func (m *Manager) StartEnabled() {
	available := m.availabilityFor(m.Sensors())

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.sensors {
		if !m.enabled[s.Name()] || !available[s.Name()] {
			continue
		}
		// Don't start if already running
		if _, running := m.cancels[s.Name()]; running {
			continue
		}

		// #nosec G118 -- cancel is kept in m.cancels and invoked by StopAll
		ctx, cancel := context.WithCancel(context.Background())
		m.cancels[s.Name()] = cancel
		alertCh := m.alertCh

		// Supervised rather than a bare goroutine: a sensor driver reads from
		// the OS — sysfs files, WMI, IOKit, netlink — and a panic in one of
		// those parsers must not take the process down while the user is away
		// believing the machine is watched. The loop comes back on its own and
		// the panic is logged.
		sensor := s
		// SuperviseRetry, not Supervise. A driver that returns an error used to
		// log it and return normally, which the supervisor read as "finished its
		// work" — so the loop was retired and never came back. m.cancels kept
		// the entry, so every later StartEnabled skipped the sensor as already
		// running. One transient failure removed it for the life of the process,
		// while the dashboard and the phone went on reporting it active and the
		// machine as armed.
		safe.SuperviseRetry(ctx, "sensor:"+sensor.Name(), func(runCtx context.Context) error {
			log.WithField("sensor", sensor.Name()).Info("Sensor started")
			m.clearFailure(sensor.Name())

			err := sensor.Start(runCtx, alertCh)

			// Being asked to stop is the one clean way out of this loop. The
			// sensor reports that as an error — usually runCtx.Err() itself —
			// and passing it on would have the supervisor restart a sensor the
			// user just disarmed or switched off.
			//
			//nolint:nilerr // cancellation is a clean stop, not a failure to retry
			if runCtx.Err() != nil {
				log.WithField("sensor", sensor.Name()).Info("Sensor stopped")
				return nil
			}
			// A sensor that returns while it is still supposed to be watching has
			// stopped watching, whether or not it says why. Returning nil here
			// would retire the loop for exactly the reason it must not be.
			if err == nil {
				err = errStoppedWatching
			}
			log.WithField("sensor", sensor.Name()).Errorf("Sensor error: %v", err)
			m.recordFailure(sensor.Name(), err)
			return err
		})
	}
}

// StopAll stops all running sensors.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, cancel := range m.cancels {
		cancel()
		delete(m.cancels, name)
		// Disarming is not a fault; see Disable.
		delete(m.failures, name)
	}

	for _, s := range m.sensors {
		_ = s.Stop()
	}
}
