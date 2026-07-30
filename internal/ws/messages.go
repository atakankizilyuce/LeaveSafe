package ws

import (
	"time"

	"github.com/leavesafe/leavesafe/internal/config"
)

const (
	MsgTypeAuth                = "auth"
	MsgTypeArm                 = "arm"
	MsgTypeDisarm              = "disarm"
	MsgTypeDisarmPin           = "disarm_with_pin"
	MsgTypeConfigure           = "configure"
	MsgTypePing                = "ping"
	MsgTypeTestAlert           = "test_alert"
	MsgTypeDismissAlarm        = "dismiss_alarm"
	MsgTypeDismissAlarmPause   = "dismiss_alarm_pause"
	MsgTypeDismissAlarmDisable = "dismiss_alarm_disable"
	MsgTypeTriggerSensor       = "trigger_sensor"
	MsgTypeGetConfig           = "get_config"
	MsgTypeUpdateConfig        = "update_config"
	MsgTypeResetConfig         = "reset_config"
	MsgTypeLocationAnchor      = "location_anchor"
	MsgTypeGetLocation         = "get_location"
)

const (
	// MsgTypeHello is the first thing the server says on a new connection. It
	// carries the certificate fingerprint so a client that arrived by QR code
	// can check it before offering the pairing key.
	MsgTypeHello             = "hello"
	MsgTypeAuthOK            = "auth_ok"
	MsgTypeAuthFail          = "auth_fail"
	MsgTypeAlert             = "alert"
	MsgTypeStatus            = "status"
	MsgTypePong              = "pong"
	MsgTypeDisconnectWarning = "disconnect_warning"
	MsgTypeAlarmActive       = "alarm_active"
	MsgTypePinRequired       = "pin_required"
	MsgTypeConfigData        = "config_data"
	MsgTypeLocation          = "location"
	// MsgTypeUpdateAvailable says a newer release exists. It is sent after
	// authentication when a result is already known, so a phone that pairs hours
	// after the check still learns about it, and broadcast when a later check
	// finds something new.
	MsgTypeUpdateAvailable = "update_available"
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
	Location *LocationFix    `json:"location,omitempty"`
}

// LocationFix is one position estimate as it crosses the wire.
type LocationFix struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
	AccuracyM float64 `json:"accuracy_m"`
	Source    string  `json:"source,omitempty"`
	Timestamp int64   `json:"ts,omitempty"`
	Label     string  `json:"label,omitempty"`
}

// LocationPayload is what the phone needs to render the location panel without
// overstating what is known: the current position, the position recorded when
// the system was armed, and how far apart they are.
type LocationPayload struct {
	// Enabled reports whether the feature is switched on at all.
	Enabled bool `json:"enabled"`
	// Available reports whether any source can actually produce a position on
	// this machine. False here with Enabled true means the panel should
	// explain itself rather than spin.
	Available bool         `json:"available"`
	Fix       *LocationFix `json:"fix,omitempty"`
	Anchor    *LocationFix `json:"anchor,omitempty"`
	MovedM    float64      `json:"moved_m,omitempty"`
	Moved     bool         `json:"moved,omitempty"`
}

// LocationConfigPayload mirrors config.Location for client exchange, minus the
// API key. Like the PIN, the key is reported as present or absent and never
// sent back out.
type LocationConfigPayload struct {
	Enabled      bool   `json:"enabled"`
	PollSeconds  int    `json:"poll_seconds"`
	PhoneAnchor  bool   `json:"phone_anchor"`
	IPFallback   bool   `json:"ip_fallback"`
	WiFiEnabled  bool   `json:"wifi_enabled"`
	GeolocateURL string `json:"geolocate_url,omitempty"`
	HasKey       bool   `json:"has_geolocate_key"`
	GeolocateKey string `json:"geolocate_key,omitempty"`
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
	Location          *LocationPayload        `json:"location,omitempty"`
	Update            *UpdatePayload          `json:"update,omitempty"`
	// CertFP is the SHA-256 fingerprint of this server's TLS certificate,
	// empty on the plain-HTTP local path where there is no certificate.
	CertFP string `json:"cert_fp,omitempty"`
}

// UpdatePayload tells the phone that a newer release exists.
//
// Command is built on the laptop, because that is the only side that knows how
// this copy was installed — and it comes from a fixed table, never from anything
// the releases endpoint returned. It is empty when the installation was not
// recognized, and the phone then offers URL instead.
type UpdatePayload struct {
	Running string `json:"running"`
	Latest  string `json:"latest"`
	URL     string `json:"url"`
	Channel string `json:"channel"`
	Command string `json:"command,omitempty"`
}

// ConfigPayload is a sanitized configuration for client exchange.
type ConfigPayload struct {
	Port                   int                   `json:"port"`
	MaxSessions            int                   `json:"max_sessions"`
	MaxAuthAttempts        int                   `json:"max_auth_attempts"`
	LockoutSeconds         int                   `json:"lockout_seconds"`
	HeartbeatSeconds       int                   `json:"heartbeat_seconds"`
	DisconnectGraceSeconds int                   `json:"disconnect_grace_seconds"`
	AutoArmOnLock          bool                  `json:"auto_arm_on_lock"`
	InputThreshold         int                   `json:"input_threshold"`
	ConnectionMode         string                `json:"connection_mode,omitempty"`
	UpdateCheck            bool                  `json:"update_check"`
	UpdateChannel          string                `json:"update_channel,omitempty"`
	UpdateCheckHours       int                   `json:"update_check_hours,omitempty"`
	Alarm                  config.AlarmConfig    `json:"alarm"`
	PinProtection          PinProtectionPayload  `json:"pin_protection"`
	EnabledSensors         map[string]bool       `json:"enabled_sensors,omitempty"`
	RemoteAccess           bool                  `json:"remote_access,omitempty"`
	RemotePort             int                   `json:"remote_port,omitempty"`
	Location               LocationConfigPayload `json:"location"`
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

// NewHello creates the greeting a fresh connection receives before it has
// authenticated. It carries no secrets: the fingerprint is public by
// definition, since it is derived from the certificate every connecting client
// is handed anyway.
func NewHello(certFP, version string) ServerMessage {
	return ServerMessage{
		Type:      MsgTypeHello,
		Version:   version,
		CertFP:    certFP,
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

// NewLocation creates a location update message.
func NewLocation(payload LocationPayload) ServerMessage {
	return ServerMessage{
		Type:      MsgTypeLocation,
		Location:  &payload,
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
