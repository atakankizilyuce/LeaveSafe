package monitor

import "context"

// AlertLevel represents the severity of an alert.
type AlertLevel string

const (
	AlertWarning  AlertLevel = "warning"
	AlertCritical AlertLevel = "critical"
)

// Alert represents a sensor event that should be reported to the user.
type Alert struct {
	Sensor  string     `json:"sensor"`
	Level   AlertLevel `json:"level"`
	Message string     `json:"message"`
}

// Sensor is the interface that all platform-specific sensors must implement.
type Sensor interface {
	// Name returns the unique identifier for this sensor (e.g., "power", "lid").
	Name() string

	// DisplayName returns a human-readable name (e.g., "Power/Charger").
	DisplayName() string

	// Available reports whether this sensor can run on the current system.
	Available() bool

	// Start begins monitoring. It should send alerts to the provided channel.
	// It blocks until the context is cancelled or an error occurs.
	Start(ctx context.Context, alerts chan<- Alert) error

	// Stop signals the sensor to stop monitoring.
	Stop() error
}
