package ws

import (
	"testing"
	"time"
)

func TestNewAlert(t *testing.T) {
	before := time.Now().Unix()
	msg := NewAlert("power", "critical", "Charger disconnected")
	after := time.Now().Unix()

	if msg.Type != MsgTypeAlert {
		t.Errorf("Type = %q, want %q", msg.Type, MsgTypeAlert)
	}
	if msg.Alert == nil {
		t.Fatal("Alert field is nil")
	}
	if msg.Alert.Sensor != "power" {
		t.Errorf("Alert.Sensor = %q, want %q", msg.Alert.Sensor, "power")
	}
	if msg.Alert.Level != "critical" {
		t.Errorf("Alert.Level = %q, want %q", msg.Alert.Level, "critical")
	}
	if msg.Alert.Message != "Charger disconnected" {
		t.Errorf("Alert.Message = %q, want %q", msg.Alert.Message, "Charger disconnected")
	}
	if msg.Timestamp < before || msg.Timestamp > after {
		t.Errorf("Timestamp %d out of expected range [%d, %d]", msg.Timestamp, before, after)
	}
}

func TestNewAuthOK(t *testing.T) {
	sensors := []SensorInfo{
		{Name: "power", DisplayName: "Power/Charger", Available: true, Enabled: true},
		{Name: "network", DisplayName: "IP Address Change", Available: true, Enabled: false},
	}
	msg := NewAuthOK("mytoken123", sensors, "1.0.0")

	if msg.Type != MsgTypeAuthOK {
		t.Errorf("Type = %q, want %q", msg.Type, MsgTypeAuthOK)
	}
	if msg.Token != "mytoken123" {
		t.Errorf("Token = %q, want %q", msg.Token, "mytoken123")
	}
	if msg.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", msg.Version, "1.0.0")
	}
	if len(msg.Sensors) != 2 {
		t.Errorf("Sensors length = %d, want 2", len(msg.Sensors))
	}
	if msg.Sensors[0].Name != "power" {
		t.Errorf("Sensors[0].Name = %q, want %q", msg.Sensors[0].Name, "power")
	}
}

func TestNewAuthOK_EmptySensors(t *testing.T) {
	msg := NewAuthOK("tok", nil, "")
	if msg.Type != MsgTypeAuthOK {
		t.Errorf("Type = %q, want %q", msg.Type, MsgTypeAuthOK)
	}
	if msg.Token != "tok" {
		t.Errorf("Token = %q, want %q", msg.Token, "tok")
	}
}

func TestNewAuthFail(t *testing.T) {
	msg := NewAuthFail("invalid key", 3)

	if msg.Type != MsgTypeAuthFail {
		t.Errorf("Type = %q, want %q", msg.Type, MsgTypeAuthFail)
	}
	if msg.Reason != "invalid key" {
		t.Errorf("Reason = %q, want %q", msg.Reason, "invalid key")
	}
	if msg.RemainingAttempts != 3 {
		t.Errorf("RemainingAttempts = %d, want 3", msg.RemainingAttempts)
	}
}

func TestNewAuthFail_ZeroRemaining(t *testing.T) {
	msg := NewAuthFail("locked out", 0)
	if msg.RemainingAttempts != 0 {
		t.Errorf("RemainingAttempts = %d, want 0", msg.RemainingAttempts)
	}
}

func TestNewStatus_Armed(t *testing.T) {
	states := map[string]*SensorState{
		"power": {Enabled: true, Status: "ok"},
	}
	before := time.Now().Unix()
	msg := NewStatus(true, states)
	after := time.Now().Unix()

	if msg.Type != MsgTypeStatus {
		t.Errorf("Type = %q, want %q", msg.Type, MsgTypeStatus)
	}
	if msg.Armed == nil {
		t.Fatal("Armed field is nil")
	}
	if !*msg.Armed {
		t.Error("Armed = false, want true")
	}
	if msg.SensorStates == nil {
		t.Fatal("SensorStates is nil")
	}
	if s, ok := msg.SensorStates["power"]; !ok {
		t.Error("SensorStates missing 'power' key")
	} else if s.Status != "ok" {
		t.Errorf("SensorStates[power].Status = %q, want %q", s.Status, "ok")
	}
	if msg.Timestamp < before || msg.Timestamp > after {
		t.Errorf("Timestamp %d out of expected range [%d, %d]", msg.Timestamp, before, after)
	}
}

func TestNewStatus_Disarmed(t *testing.T) {
	msg := NewStatus(false, nil)
	if msg.Armed == nil {
		t.Fatal("Armed field is nil")
	}
	if *msg.Armed {
		t.Error("Armed = true, want false")
	}
}

func TestMsgTypeConstants(t *testing.T) {
	// Client→Server types
	if MsgTypeAuth != "auth" {
		t.Errorf("MsgTypeAuth = %q, want %q", MsgTypeAuth, "auth")
	}
	if MsgTypeArm != "arm" {
		t.Errorf("MsgTypeArm = %q, want %q", MsgTypeArm, "arm")
	}
	if MsgTypeDisarm != "disarm" {
		t.Errorf("MsgTypeDisarm = %q, want %q", MsgTypeDisarm, "disarm")
	}
	if MsgTypeConfigure != "configure" {
		t.Errorf("MsgTypeConfigure = %q, want %q", MsgTypeConfigure, "configure")
	}
	if MsgTypePing != "ping" {
		t.Errorf("MsgTypePing = %q, want %q", MsgTypePing, "ping")
	}
	if MsgTypeTestAlert != "test_alert" {
		t.Errorf("MsgTypeTestAlert = %q, want %q", MsgTypeTestAlert, "test_alert")
	}
	// Server→Client types
	if MsgTypeAuthOK != "auth_ok" {
		t.Errorf("MsgTypeAuthOK = %q, want %q", MsgTypeAuthOK, "auth_ok")
	}
	if MsgTypeAuthFail != "auth_fail" {
		t.Errorf("MsgTypeAuthFail = %q, want %q", MsgTypeAuthFail, "auth_fail")
	}
	if MsgTypeAlert != "alert" {
		t.Errorf("MsgTypeAlert = %q, want %q", MsgTypeAlert, "alert")
	}
	if MsgTypeStatus != "status" {
		t.Errorf("MsgTypeStatus = %q, want %q", MsgTypeStatus, "status")
	}
	if MsgTypePong != "pong" {
		t.Errorf("MsgTypePong = %q, want %q", MsgTypePong, "pong")
	}
	if MsgTypeDisconnectWarning != "disconnect_warning" {
		t.Errorf("MsgTypeDisconnectWarning = %q, want %q", MsgTypeDisconnectWarning, "disconnect_warning")
	}
}
