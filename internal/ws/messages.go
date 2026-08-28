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
	// MsgTypePushSubscribe hands over somewhere to reach this phone when it is
	// not connected, which is the one time it cannot be told anything.
	MsgTypePushSubscribe = "push_subscribe"
)

const (
	// MsgTypeHello is the first thing the server says on a new connection. It
	// names the version, so a client knows what it reached, and carries the
	// challenge the client has to answer before it is paired.
	MsgTypeHello             = "hello"
	MsgTypeAuthOK            = "auth_ok"
	MsgTypeAuthFail          = "auth_fail"
	MsgTypeAlert             = "alert"
	MsgTypeStatus            = "status"
	MsgTypePong              = "pong"
	MsgTypeDisconnectWarning = "disconnect_warning"
	MsgTypeAlarmActive       = "alarm_active"
	// MsgTypeAlarmCleared says the alarm has been dismissed by someone other
	// than this phone — from the laptop's own terminal, or by another paired
	// phone. Without it a console `stop` silences the laptop and leaves every
	// phone sounding.
	MsgTypeAlarmCleared = "alarm_cleared"
	MsgTypePinRequired  = "pin_required"
	MsgTypeConfigData   = "config_data"
	MsgTypeLocation     = "location"
	// MsgTypeUpdateAvailable says a newer release exists. It is sent after
	// authentication when a result is already known, so a phone that pairs hours
	// after the check still learns about it, and broadcast when a later check
	// finds something new.
	MsgTypeUpdateAvailable = "update_available"
)

// ClientMessage represents a message from the phone to the laptop.
type ClientMessage struct {
	Type string `json:"type"`
	// Key is the pairing key in plaintext, and only released apps still send
	// it. A current app answers the greeting's challenge with Nonce and Proof
	// instead, so the key never crosses the wire at all.
	Key   string `json:"key,omitempty"`
	Token string `json:"token,omitempty"`
	// Nonce on an auth message is the client's half of the pairing challenge
	// and Proof is its answer to the server's half, both hex-encoded.
	// handshakeProof spells out what is signed and why both nonces are in it.
	Nonce    string          `json:"nonce,omitempty"`
	Proof    string          `json:"proof,omitempty"`
	Pin      string          `json:"pin,omitempty"`
	Sensors  map[string]bool `json:"sensors,omitempty"`
	Sensor   string          `json:"sensor,omitempty"`
	Duration int             `json:"duration,omitempty"`
	Config   *ConfigPayload  `json:"config,omitempty"`
	Location *LocationFix    `json:"location,omitempty"`
	Push     *PushSub        `json:"push,omitempty"`
}

// PushSub is what a browser's PushManager produced, on its way to being
// somewhere this laptop can reach the phone from behind any NAT.
//
// The three parts are the browser's own: a URL at its push service, the public
// half of a key pair whose private half never leaves the phone, and a secret
// the phone generated for this subscription. The laptop keeps all three and can
// read none of what it later sends with them — the encryption is to the phone,
// not to the service carrying it.
type PushSub struct {
	Endpoint string `json:"endpoint"`
	Key      string `json:"key"`
	Auth     string `json:"auth"`
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
	Type    string `json:"type"`
	Token   string `json:"token,omitempty"`
	Version string `json:"version,omitempty"`
	// Nonce on a hello is this connection's challenge, one per connection and
	// never reused. Proof on an auth_ok is the laptop's answer to the client's
	// challenge — the field that lets an app tell this daemon apart from
	// anything else that could have claimed its port. Neither appears on any
	// other message.
	Nonce             string       `json:"nonce,omitempty"`
	Proof             string       `json:"proof,omitempty"`
	Reason            string       `json:"reason,omitempty"`
	RemainingAttempts int          `json:"remaining_attempts,omitempty"`
	Sensors           []SensorInfo `json:"sensors,omitempty"`
	// Addresses is where a phone could actually reach this machine, as
	// whole URLs. Sent on auth_ok and nowhere else: the desktop application
	// knows the port from endpoint.json but only ever has 127.0.0.1 for the
	// host, which is right and which no phone can dial. Working out the rest
	// means knowing which interfaces are up, which are loopback, and which
	// are the bridges Docker, WSL and Hyper-V leave behind — and that is
	// written once, in internal/server, rather than a second time in
	// whatever language the client happens to be.
	//
	// Not on the greeting. A list of the addresses a machine answers on is a
	// map of somebody's network, and it is not owed to a stranger who has
	// only managed to reach one of them.
	Addresses    []string                `json:"addresses,omitempty"`
	SensorStates map[string]*SensorState `json:"sensor_states,omitempty"`
	Armed        *bool                   `json:"armed,omitempty"`
	// ArmedSince is when arming happened, in Unix seconds. Sent with auth_ok so
	// a phone that reconnected resumes its counter instead of restarting it.
	// Omitted when the machine is not armed.
	ArmedSince *int64           `json:"armed_since,omitempty"`
	Alert      *AlertData       `json:"alert,omitempty"`
	Timestamp  int64            `json:"ts,omitempty"`
	Config     *ConfigPayload   `json:"config,omitempty"`
	Location   *LocationPayload `json:"location,omitempty"`
	Update     *UpdatePayload   `json:"update,omitempty"`
	// PushKey is the public half of the key this laptop signs push messages
	// with, which the phone needs before it can subscribe to anything.
	//
	// It travels with the successful pairing rather than with the greeting. It
	// is not a secret — it is meant to be handed out, and a browser writes it
	// into the subscription for anyone to read — but there is no reason to
	// answer it to a connection that has not proved it holds the pairing key.
	PushKey string `json:"push_key,omitempty"`
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
	// Failure says why this sensor is not watching, empty when it is. A sensor
	// can be enabled and available and still not running — its driver failed and
	// the loop is being restarted — and without this the panel reported it as
	// covered. For an alarm, claimed coverage that does not exist is the one
	// error worth going out of the way to avoid.
	Failure string `json:"failure,omitempty"`
}

// SensorState represents the current state of a sensor.
type SensorState struct {
	Enabled bool `json:"enabled"`
	// Status is "ok", "unavailable" when the machine has no such sensor, or
	// "failed" when it has one and the driver is not running.
	Status  string `json:"status"`
	Failure string `json:"failure,omitempty"`
}

// AlertData represents an alert event.
type AlertData struct {
	Sensor  string `json:"sensor"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// SensorSystem is the sensor name an alert carries when it is the laptop
// talking about itself rather than reporting something that touched it: a
// setting that needs a restart, a change it would not make while armed.
//
// No sensor is registered under it, and that is what the phone reads it by. A
// notice sent this way is shown as one; anything else raises the alarm.
const SensorSystem = "system"

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
// authenticated. It carries no secrets — the version, so a phone can say what
// it is talking to, and this connection's challenge, which is a random number
// and worth nothing to anyone who does not hold the pairing key.
//
// The nonce is what makes the greeting more than an introduction: the client
// answers it with a proof instead of the key, and demands one back. See
// handshakeProof.
func NewHello(version, nonce string) ServerMessage {
	return ServerMessage{
		Type:      MsgTypeHello,
		Version:   version,
		Nonce:     nonce,
		Timestamp: time.Now().Unix(),
	}
}

// NewAuthOK creates an auth success response.
//
// It carries the armed state because a reconnecting phone would otherwise show
// the machine as disarmed until the next status broadcast — and the commonest
// reason to reconnect is that the screen locked, which is exactly when the
// machine is most likely to be armed.
func NewAuthOK(token string, sensors []SensorInfo, version string, armed bool, armedAt time.Time) ServerMessage {
	msg := ServerMessage{
		Type:    MsgTypeAuthOK,
		Token:   token,
		Version: version,
		Sensors: sensors,
		Armed:   &armed,
	}
	if armed && !armedAt.IsZero() {
		since := armedAt.Unix()
		msg.ArmedSince = &since
	}
	return msg
}

// NewAuthFail creates an auth failure response.
func NewAuthFail(reason string, remaining int) ServerMessage {
	return ServerMessage{
		Type:              MsgTypeAuthFail,
		Reason:            reason,
		RemainingAttempts: remaining,
	}
}

// NewAlarmActive creates a message indicating the laptop alarm is sounding,
// as of now.
func NewAlarmActive(sensor, message string) ServerMessage {
	return NewAlarmActiveAt(sensor, message, time.Now())
}

// NewAlarmActiveAt is the same message for an alarm that started earlier.
//
// The one place it is not now is the one that matters most. A phone drops its
// socket every time its screen locks, so reconnecting into an alarm already
// sounding is the ordinary way to meet one — and that message used to carry the
// moment it was sent. The phone believes the daemon about its own alarm over
// its own clock, and rightly, so a trip four minutes old arrived reading "just
// now": the panel said the wrong thing about the one fact somebody standing in
// front of it is trying to establish.
func NewAlarmActiveAt(sensor, message string, at time.Time) ServerMessage {
	return ServerMessage{
		Type: MsgTypeAlarmActive,
		Alert: &AlertData{
			Sensor:  sensor,
			Message: message,
		},
		Timestamp: at.Unix(),
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
