package ws

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
)

// dialAndAuth opens a socket, gets past the greeting and pairs it, returning a
// connection the caller closes to simulate the phone going away.
func dialAndAuth(t *testing.T, ctx context.Context, srv string, hub *Hub) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, srv, nil) //nolint:bodyclose // response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	readHello(t, ctx, conn)

	authMsg := `{"type":"auth","key":"` + hub.authManager.RawPairingKey() + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(authMsg)); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("expected auth_ok: %v", err)
	}
	return conn
}

// A phone whose screen locks drops the socket. That is the ordinary case, not an
// intrusion, and treating it as one means the laptop screams at a user who did
// nothing but put their phone in their pocket.
func TestDisconnectWhileArmedDoesNotAlarm(t *testing.T) {
	const grace = 50 * time.Millisecond

	hub := testHub(t)
	hub.SetTimings(0, grace)

	var alarmed, reported atomic.Bool
	hub.SetAlarmTriggerCallback(func() { alarmed.Store(true) })
	hub.SetAllDisconnectedCallback(func() { reported.Store(true) })

	srv := hubServer(t, hub)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAndAuth(t, ctx, wsURL(srv), hub)
	hub.Arm()

	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("close: %v", err)
	}

	time.Sleep(grace * 6)

	if alarmed.Load() {
		t.Error("losing the phone started the alarm; only a real sensor event should")
	}
	if !reported.Load() {
		t.Error("losing the phone was never reported")
	}
}

// A phone that reconnects within the grace period never really left, so it must
// produce no notice at all. Otherwise every brief network blip writes a line.
func TestReconnectWithinGraceIsNotReported(t *testing.T) {
	const grace = 300 * time.Millisecond

	hub := testHub(t)
	hub.SetTimings(0, grace)

	var reported atomic.Bool
	hub.SetAllDisconnectedCallback(func() { reported.Store(true) })

	srv := hubServer(t, hub)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAndAuth(t, ctx, wsURL(srv), hub)
	hub.Arm()

	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Back before the grace period is up, the way a page that reloaded is.
	again := dialAndAuth(t, ctx, wsURL(srv), hub)
	defer again.Close(websocket.StatusNormalClosure, "")

	time.Sleep(grace * 3)

	if reported.Load() {
		t.Error("a phone that came straight back was reported as gone")
	}
}

// firingSensor reports one event as soon as it is started, standing in for a
// charger being pulled while the machine is armed.
type firingSensor struct{ name string }

func (s *firingSensor) Name() string        { return s.name }
func (s *firingSensor) DisplayName() string { return s.name }
func (s *firingSensor) Available() bool     { return true }
func (s *firingSensor) Stop() error         { return nil }

func (s *firingSensor) Start(ctx context.Context, alerts chan<- monitor.Alert) error {
	alerts <- monitor.Alert{
		Sensor:  s.name,
		Level:   monitor.AlertCritical,
		Message: "Charger disconnected",
	}
	<-ctx.Done()
	return nil
}

// The alarm still has to fire for the thing it is actually for. Task 1 removed
// one route to fireAlarmTrigger, and this pins down that it left the route that
// matters — a real sensor event on an armed machine — alone.
func TestArmedHubStillAlarmsOnASensorEvent(t *testing.T) {
	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}

	mgr := monitor.NewManager()
	mgr.Register(&firingSensor{name: "power"})
	mgr.Enable("power")

	hub := NewHub(authMgr, mgr, "test")

	var alarmed atomic.Bool
	hub.SetAlarmTriggerCallback(func() { alarmed.Store(true) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go hub.RunAlertDispatcher(ctx)

	// Arming starts the enabled sensors, and this one reports immediately.
	hub.Arm()

	deadline := time.Now().Add(2 * time.Second)
	for !alarmed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !alarmed.Load() {
		t.Error("a real sensor event did not start the alarm")
	}
}
