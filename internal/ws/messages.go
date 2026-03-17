package ws

import "time"

// Message types for client-to-server communication.
const (
	MsgTypeAuth      = "auth"
	MsgTypeArm       = "arm"
	MsgTypeDisarm    = "disarm"
	MsgTypeConfigure = "configure"
	MsgTypePing      = "ping"
	MsgTypeTestAlert = "test_alert"
)

// Message types for server-to-client communication.
const (
	MsgTypeAuthOK            = "auth_ok"
	MsgTypeAuthFail          = "auth_fail"
	MsgTypeAlert             = "alert"
	MsgTypeStatus            = "status"
	MsgTypePong              = "pong"
	MsgTypeDisconnectWarning = "disconnect_warning"
)

// ClientMessage represents a message from the phone to the laptop.
type ClientMessage struct {
	Type    string            `json:"type"`
	Key     string            `json:"key,omitempty"`
	Token   string            `json:"token,omitempty"`
	Sensors map[string]bool   `json:"sensors,omitempty"`
}

// ServerMessage represents a message from the laptop to the phone.
type ServerMessage struct {
	Type              string                  `json:"type"`
	Token             string                  `json:"token,omitempty"`
	Reason            string                  `json:"reason,omitempty"`
	RemainingAttempts int                     `json:"remaining_attempts,omitempty"`
	Sensors           []SensorInfo            `json:"sensors,omitempty"`
	SensorStates      map[string]*SensorState `json:"sensor_states,omitempty"`
	Armed             *bool                   `json:"armed,omitempty"`
	Alert             *AlertData              `json:"alert,omitempty"`
	Timestamp         int64                   `json:"ts,omitempty"`
}

// SensorInfo describes an available sensor.
type SensorInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Available   bool   `json:"available"`
	Enabled     bool   `json:"enabled"`
}

// SensorState represents the current state of a sensor.
type SensorState struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"` // "ok", "alert", "unavailable"
}

// AlertData represents an alert event.
type AlertData struct {
	Sensor  string `json:"sensor"`
	Level   string `json:"level"`   // "warning", "critical"
	Message string `json:"message"`
}

// NewAlert creates a new server alert message.
func NewAlert(sensor, level, message string) ServerMessage {
	return ServerMessage{
		Type: MsgTypeAlert,
		Alert: &AlertData{
			Sensor:  sensor,
			Level:   level,
			Message: message,
		},
		Timestamp: time.Now().Unix(),
	}
}

// NewAuthOK creates an auth success response.
func NewAuthOK(token string, sensors []SensorInfo) ServerMessage {
	return ServerMessage{
		Type:    MsgTypeAuthOK,
		Token:   token,
		Sensors: sensors,
	}
}

// NewAuthFail creates an auth failure response.
func NewAuthFail(reason string, remaining int) ServerMessage {
	return ServerMessage{
		Type:              MsgTypeAuthFail,
		Reason:            reason,
		RemainingAttempts: remaining,
	}
}

// NewStatus creates a status update message.
func NewStatus(armed bool, states map[string]*SensorState) ServerMessage {
	return ServerMessage{
		Type:         MsgTypeStatus,
		Armed:        &armed,
		SensorStates: states,
		Timestamp:    time.Now().Unix(),
	}
}
