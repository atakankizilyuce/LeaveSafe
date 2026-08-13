package ws

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/location"
)

// A settings update decides a great deal under the hub's lock and can act on
// almost none of it there: the file, the sensor manager, every paired phone and
// an internet-facing listener all have to be reached with the lock released.
// These are the paths where the decision and the acting come apart — a refusal,
// a change with nothing to apply it to, and a write that fails.

// unwritableConfigDir points the config directory at a plain file, so anything
// trying to create it fails. TestMain has already moved the whole package off
// the developer's real config; this moves one test somewhere unusable on
// purpose.
func unwritableConfigDir(t *testing.T) {
	t.Helper()
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("in the way"), 0o600); err != nil {
		t.Fatalf("write the blocking file: %v", err)
	}
	for _, name := range []string{"APPDATA", "HOME", "USERPROFILE"} {
		t.Setenv(name, blocked)
	}
}

// A hub with no configuration behind it is the window between starting up and
// loading the file. A settings update arriving then has nothing to write into,
// and must not invent one.
func TestASettingsUpdateWithNoConfigurationBehindItIsDropped(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	rec := listeningClient(t, hub)

	payload := configToPayload(config.Default())
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	if len(rec.warnings()) != 0 {
		t.Errorf("an update with nothing to update announced %q", rec.warnings())
	}
}

// The PIN guards the settings as well as the disarm, because turning PIN
// protection off is the same thing as disarming with extra steps.
func TestChangingThePINWithoutTheCurrentOneIsRefused(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	cfg := config.Default()
	cfg.PinProtection.Enabled = true
	cfg.PinProtection.PinHash = "a-hash-that-nothing-matches"
	hub.SetConfig(cfg)

	rec := &alertRecorder{}
	client := &Client{hub: hub, transport: rec, authenticated: true, remoteAddr: "203.0.113.9"}
	hub.mu.Lock()
	hub.clients[client] = true
	hub.mu.Unlock()

	payload := configToPayload(cfg)
	payload.PinProtection.Enabled = false
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload, Pin: "0000"}, client)

	if _, ok := rec.saw(MsgTypePinRequired); !ok {
		t.Fatal("switching PIN protection off did not ask for the current PIN")
	}
	hub.mu.RLock()
	stillOn := hub.cfg.PinProtection.Enabled
	hub.mu.RUnlock()
	if !stillOn {
		t.Error("PIN protection was switched off without the PIN")
	}
}

// saw is the same question alertRecorder's warnings() asks, for message types
// that carry no alert.
func (a *alertRecorder) saw(msgType string) (ServerMessage, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, m := range a.sent {
		if m.Type == msgType {
			return m, true
		}
	}
	return ServerMessage{}, false
}

// The phone is a client like any other and its numbers are not trusted further
// than a hand-edited file's would be. A zero heartbeat would be a ticker panic
// on the next restart.
func TestImpossibleNumbersFromThePhoneAreCorrectedBeforeTheyAreStored(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	hub.SetConfig(config.Default())
	listeningClient(t, hub)

	payload := configToPayload(config.Default())
	payload.HeartbeatSeconds = 0
	payload.MaxAuthAttempts = -4
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	hub.mu.RLock()
	heartbeat, attempts := hub.cfg.HeartbeatSeconds, hub.cfg.MaxAuthAttempts
	hub.mu.RUnlock()
	if heartbeat <= 0 {
		t.Errorf("a zero heartbeat was stored as %d", heartbeat)
	}
	if attempts <= 0 {
		t.Errorf("a negative attempt limit was stored as %d", attempts)
	}
}

// The settings the phone sent are in force the moment they are applied, whether
// or not the file they belong in could be written. Refusing them because the
// disk said no would leave the running laptop and the phone disagreeing about
// what is switched on.
func TestSettingsStayInForceWhenTheyCannotBeWrittenToDisk(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	hub.SetConfig(config.Default())
	rec := listeningClient(t, hub)
	unwritableConfigDir(t)

	payload := configToPayload(config.Default())
	payload.Port = config.Default().Port + 7
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	hub.mu.RLock()
	port := hub.cfg.Port
	hub.mu.RUnlock()
	if port != payload.Port {
		t.Errorf("the port was not applied; it is %d", port)
	}
	if !rec.sawWarning("restart required") {
		t.Errorf("the port change was not reported as needing a restart; alerts were %q", rec.warnings())
	}
}

// An https endpoint is accepted, and accepting it clears the stored API key
// unless the client supplied one to go with it: a client could otherwise point
// the laptop at a server it controls and collect the key on the next resolve.
func TestANewGeolocationEndpointDoesNotInheritTheStoredKey(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	cfg := config.Default()
	cfg.Location.GeolocateKey = "the-key-that-belongs-to-the-old-endpoint"
	hub.SetConfig(cfg)
	listeningClient(t, hub)

	const secure = "https://geolocate.example.com/v1"
	payload := configToPayload(cfg)
	payload.Location.GeolocateURL = secure
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	if got := geolocateURL(hub); got != secure {
		t.Errorf("the https endpoint was not stored; it is %q", got)
	}
	hub.mu.RLock()
	key := hub.cfg.Location.GeolocateKey
	hub.mu.RUnlock()
	if key != "" {
		t.Errorf("the new endpoint inherited the old key %q", key)
	}
}

func TestAnEndpointSuppliedWithItsOwnKeyKeepsThatKey(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	cfg := config.Default()
	cfg.Location.GeolocateKey = "the-old-key"
	hub.SetConfig(cfg)
	listeningClient(t, hub)

	payload := configToPayload(cfg)
	payload.Location.GeolocateURL = "https://geolocate.example.com/v1"
	payload.Location.GeolocateKey = "the-new-key"
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	hub.mu.RLock()
	key := hub.cfg.Location.GeolocateKey
	hub.mu.RUnlock()
	if key != "the-new-key" {
		t.Errorf("the supplied key was not stored; it is %q", key)
	}
}

// Switching a sensor from the panel goes through its own message rather than
// the settings sheet, and it is refused while armed for the same reason.
func TestSwitchingASensorFromThePanel(t *testing.T) {
	hub := triggerHub(t)
	hub.SetConfig(config.Default())
	client, _ := hub.pairRecorder(t)

	hub.handleMessage(client, ClientMessage{
		Type:    MsgTypeConfigure,
		Sensors: map[string]bool{"power": false},
	})

	if hub.sensorMgr.IsEnabled("power") {
		t.Error("the sensor was not switched off")
	}
}

// An alarm is exactly when the position matters, so it goes out with the alert
// rather than waiting for the next poll.
func TestTheAlarmCarriesThePositionWithIt(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	rec := listeningClient(t, hub)
	hub.SetLocationTracker(context.Background(), location.NewTracker(nil, time.Minute))
	hub.Arm()

	hub.dispatchAlert(alertFrom("power", "charger disconnected"))

	if _, ok := rec.saw(MsgTypeLocation); !ok {
		t.Error("the alarm went out without the position")
	}
}

// A screen event that is neither the display going off nor coming on is not an
// auto-arm decision, and it must fall through to being an ordinary alert rather
// than being swallowed.
//
// The message is chosen with care: the hub matches "off" and "on" as substrings
// of whatever the sensor said, so a great many ordinary words — "reconfigured",
// "connected" — read as the display coming on.
func TestAScreenEventThatIsNeitherLockNorUnlockIsJustAnAlert(t *testing.T) {
	hub, _ := hubWithSensors(t, "screen")
	rec := listeningClient(t, hub)
	hub.SetAutoArmOnLock(true)
	hub.Arm()

	hub.dispatchAlert(alertFrom("screen", "the display went dark"))

	if !hub.IsArmed() {
		t.Error("an unrelated screen event disarmed the machine")
	}
	if !rec.sawWarning("went dark") {
		t.Errorf("an unrelated screen event was swallowed; alerts were %q", rec.warnings())
	}
}
