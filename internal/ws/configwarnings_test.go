package ws

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/monitor"
)

// A configuration handler that refuses a change, or accepts one that only takes
// effect on the next start, has to say so. The phone shows the settings sheet as
// saved either way, so a change that went nowhere is indistinguishable from one
// that worked unless a warning arrives. These tests are about those warnings,
// and about the sensors a reset to defaults has to reach.

// alertRecorder is a transport that keeps what the hub sent rather than only
// counting it, so a test can ask which warning was raised.
type alertRecorder struct {
	mu   sync.Mutex
	sent []ServerMessage
}

func (a *alertRecorder) Send(data []byte) error {
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, msg)
	return nil
}

func (a *alertRecorder) Close() error { return nil }

// warnings returns the text of every alert the hub pushed to this client.
func (a *alertRecorder) warnings() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, msg := range a.sent {
		if msg.Type == MsgTypeAlert && msg.Alert != nil {
			out = append(out, msg.Alert.Message)
		}
	}
	return out
}

// sawWarning reports whether any alert mentioned the given phrase.
func (a *alertRecorder) sawWarning(phrase string) bool {
	for _, msg := range a.warnings() {
		if strings.Contains(msg, phrase) {
			return true
		}
	}
	return false
}

// hubWithSensors builds a hub whose manager knows the named sensors. They start
// disabled, which is what the manager does with anything registered.
func hubWithSensors(t *testing.T, names ...string) (*Hub, []*quietSensor) {
	t.Helper()
	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	mgr := monitor.NewManager()
	sensors := make([]*quietSensor, 0, len(names))
	for _, name := range names {
		s := &quietSensor{name: name}
		sensors = append(sensors, s)
		mgr.Register(s)
	}
	t.Cleanup(mgr.StopAll)
	return NewHub(authMgr, mgr, "test"), sensors
}

// listeningClient attaches an authenticated client to the hub and hands back
// what it received. PushAlert only reaches authenticated clients, so a client
// that had not got that far would make every warning assertion vacuous.
func listeningClient(t *testing.T, hub *Hub) *alertRecorder {
	t.Helper()
	rec := &alertRecorder{}
	client := &Client{hub: hub, transport: rec, authenticated: true}
	hub.mu.Lock()
	hub.clients[client] = true
	hub.mu.Unlock()
	return rec
}

// waitUntil polls a condition that a goroutine satisfies, because starting a
// sensor happens off the handler's own goroutine.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(what)
}

// geolocateURL reads the stored endpoint. The hub has no getter for it, and the
// field is guarded, so the test takes the lock the same way the hub does.
func geolocateURL(hub *Hub) string {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	if hub.cfg == nil {
		return ""
	}
	return hub.cfg.Location.GeolocateURL
}

// Sensors cannot be switched while the system is armed, because the change
// would land on a machine the user has already walked away from.
func TestConfiguringSensorsWhileArmedIsRefusedWithAWarning(t *testing.T) {
	hub, _ := hubWithSensors(t, "power", "lid")
	hub.SetConfig(config.Default())
	hub.sensorMgr.Enable("power")
	rec := listeningClient(t, hub)
	hub.Arm()

	hub.handleConfigure(ClientMessage{Type: MsgTypeConfigure, Sensors: map[string]bool{"power": false}})

	if !rec.sawWarning("disarm first") {
		t.Errorf("configuring while armed raised no warning; alerts were %q", rec.warnings())
	}
	// The refusal has to be a refusal. A warning beside a change that happened
	// anyway would be worse than silence.
	if !hub.sensorMgr.IsEnabled("power") {
		t.Error("the sensor was switched off despite the change being refused")
	}
}

func TestChangingThePortWarnsThatARestartIsNeeded(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	cfg := config.Default()
	hub.SetConfig(cfg)
	rec := listeningClient(t, hub)

	payload := configToPayload(cfg)
	payload.Port = cfg.Port + 1
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	if !rec.sawWarning("restart required") {
		t.Errorf("a changed port did not warn about the restart; alerts were %q", rec.warnings())
	}
}

// The geolocation API key travels in that URL's query string, so a plain-HTTP
// endpoint would put it on the wire in cleartext. The change is dropped, and
// dropping it silently would leave the phone showing an endpoint the laptop
// never accepted.
func TestAPlainHTTPGeolocationEndpointIsRejectedWithAWarning(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	cfg := config.Default()
	hub.SetConfig(cfg)
	rec := listeningClient(t, hub)

	const plain = "http://geolocate.example.com/v1"
	payload := configToPayload(cfg)
	payload.Location.GeolocateURL = plain
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	if !rec.sawWarning("https://") {
		t.Errorf("a plain-HTTP endpoint was not reported as rejected; alerts were %q", rec.warnings())
	}
	if geolocateURL(hub) == plain {
		t.Error("the plain-HTTP endpoint was stored despite being rejected")
	}
}

// The tracker builds its providers once at startup, so switching a location
// source on does nothing until the next start.
func TestChangingLocationSettingsWarnsThatARestartIsNeeded(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	cfg := config.Default()
	hub.SetConfig(cfg)
	rec := listeningClient(t, hub)

	payload := configToPayload(cfg)
	payload.Location.WiFiEnabled = !cfg.Location.WiFiEnabled
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	if !rec.sawWarning("Location settings changed") {
		t.Errorf("a location change did not warn about the restart; alerts were %q", rec.warnings())
	}
}

// A reset writes defaults that describe every sensor watching. The manager has
// to be told, or a sensor stays switched off while the configuration it just
// produced says nothing of the sort — a disagreement that lasts until the next
// restart reads the file and quietly switches it back on.
func TestResettingToDefaultsSwitchesEverySensorBackOn(t *testing.T) {
	hub, _ := hubWithSensors(t, "power", "lid")
	hub.SetConfig(config.Default())
	hub.sensorMgr.Enable("power")
	hub.sensorMgr.Disable("lid")

	hub.handleResetConfig(ClientMessage{Type: MsgTypeResetConfig}, &Client{hub: hub})

	for _, name := range []string{"power", "lid"} {
		if !hub.sensorMgr.IsEnabled(name) {
			t.Errorf("%s is still switched off after a reset to defaults", name)
		}
	}
}

// Enabling a sensor on an armed machine is only half the job: nothing is
// watching until the manager is told to start what it has just switched on.
func TestResettingToDefaultsWhileArmedStartsTheSensorsItEnabled(t *testing.T) {
	hub, sensors := hubWithSensors(t, "power", "lid")
	hub.SetConfig(config.Default())
	hub.Arm()

	hub.handleResetConfig(ClientMessage{Type: MsgTypeResetConfig}, &Client{hub: hub})

	waitUntil(t, "the sensors a reset switched on were never started", func() bool {
		for _, s := range sensors {
			if s.turns.Load() == 0 {
				return false
			}
		}
		return true
	})
}

func TestResettingAChangedPortWarnsThatARestartIsNeeded(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	cfg := config.Default()
	cfg.Port++
	hub.SetConfig(cfg)
	rec := listeningClient(t, hub)

	hub.handleResetConfig(ClientMessage{Type: MsgTypeResetConfig}, &Client{hub: hub})

	if !rec.sawWarning("restart required") {
		t.Errorf("resetting a changed port did not warn about the restart; alerts were %q", rec.warnings())
	}
}
