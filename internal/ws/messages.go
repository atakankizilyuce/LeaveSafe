package ws

import (
	"time"

	"github.com/leavesafe/leavesafe/internal/config"
)

const (
	MsgTypeAuth         = "auth"
	MsgTypeArm          = "arm"
	MsgTypeDisarm       = "disarm"
	MsgTypeDisarmPin    = "disarm_with_pin"
	MsgTypeConfigure    = "configure"
	MsgTypePing         = "ping"
	MsgTypeTestAlert    = "test_alert"
	MsgTypeDismissAlarm         = "dismiss_alarm"
	MsgTypeDismissAlarmPause    = "dismiss_alarm_pause"
	MsgTypeDismissAlarmDisable  = "dismiss_alarm_disable"
	MsgTypeTriggerSensor        = "trigger_sensor"
	MsgTypeGetConfig            = "get_config"
	MsgTypeUpdateConfig         = "update_config"
)

const (
	MsgTypeAuthOK            = "auth_ok"
	MsgTypeAuthFail          = "auth_fail"
	MsgTypeAlert             = "alert"
	MsgTypeStatus            = "status"
	MsgTypePong              = "pong"
	MsgTypeDisconnectWarning = "disconnect_warning"
	MsgTypeAlarmActive       = "alarm_active"
	MsgTypePinRequired       = "pin_required"
	MsgTypeConfigData        = "config_data"
)

// ClientMessage represents a message from the phone to the laptop.
type ClientMessage struct {
	Type     string          `json:"type"`
	Key      string          `json:"key,omitempty"`
	Token    string          `json:"token,omitempty"`
	Pin      string          `json:"pin,omitempty"`
	Sensors  map[string]bool `json:"sensors,omitempty"`
	Sensor   string          `json:"sensor,omitempty"`
	Duration int             `json:"duration,omitempty"`
	Config   *ConfigPayload  `json:"config,omitempty"`
}

// ServerMessage represents a message from the laptop to the phone.
type ServerMessage struct {
	Type              string                  `json:"type"`
	Token             string                  `json:"token,omitempty"`
	Version           string                  `json:"version,omitempty"`
	Reason            string                  `json:"reason,omitempty"`
	RemainingAttempts int                     `json:"remaining_attempts,omitempty"`
	Sensors           []SensorInfo            `json:"sensors,omitempty"`
	SensorStates      map[string]*SensorState `json:"sensor_states,omitempty"`
	Armed             *bool                   `json:"armed,omitempty"`
	Alert             *AlertData              `json:"alert,omitempty"`
	Timestamp         int64                   `json:"ts,omitempty"`
	Config            *ConfigPayload          `json:"config,omitempty"`
}

// ConfigPayload is a sanitized configuration for client exchange.
type ConfigPayload struct {
	Port                   int                  `json:"port"`
	MaxSessions            int                  `json:"max_sessions"`
	MaxAuthAttempts        int                  `json:"max_auth_attempts"`
	LockoutSeconds         int                  `json:"lockout_seconds"`
	HeartbeatSeconds       int                  `json:"heartbeat_seconds"`
	DisconnectGraceSeconds int                  `json:"disconnect_grace_seconds"`
	AutoArmOnLock          bool                 `json:"auto_arm_on_lock"`
	InputThreshold         int                  `json:"input_threshold"`
	ConnectionMode         string               `json:"connection_mode,omitempty"`
	Alarm                  config.AlarmConfig   `json:"alarm"`
	PinProtection          PinProtectionPayload `json:"pin_protection"`
	EnabledSensors         map[string]bool      `json:"enabled_sensors,omitempty"`
}

// PinProtectionPayload is the PIN config for client exchange.
type PinProtectionPayload struct {
	Enabled bool   `json:"enabled"`
	HasPin  bool   `json:"has_pin,omitempty"`
	Pin     string `json:"pin,omitempty"`
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
	Status  string `json:"status"`
}

// AlertData represents an alert event.
type AlertData struct {
	Sensor  string `json:"sensor"`
	Level   string `json:"level"`
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
func NewAuthOK(token string, sensors []SensorInfo, version string) ServerMessage {
	return ServerMessage{
		Type:    MsgTypeAuthOK,
		Token:   token,
		Version: version,
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

// NewAlarmActive creates a message indicating the laptop alarm is sounding.
func NewAlarmActive(sensor, message string) ServerMessage {
	return ServerMessage{
		Type: MsgTypeAlarmActive,
		Alert: &AlertData{
			Sensor:  sensor,
			Message: message,
		},
		Timestamp: time.Now().Unix(),
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
