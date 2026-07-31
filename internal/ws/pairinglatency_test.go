package ws

import (
	"context"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
)

// slowProbeSensor stands in for the Windows lid sensor, whose Available()
// starts a PowerShell process to ask WMI whether the machine has a battery.
// That answer is cached after the first call, so exactly one caller pays for it
// — and which caller that is has never been decided anywhere.
type slowProbeSensor struct {
	probe time.Duration
	done  chan struct{}
}

func newSlowProbeSensor(probe time.Duration) *slowProbeSensor {
	return &slowProbeSensor{probe: probe, done: make(chan struct{})}
}

func (s *slowProbeSensor) Name() string        { return "slow" }
func (s *slowProbeSensor) DisplayName() string { return "Slow Probe" }

func (s *slowProbeSensor) Available() bool {
	select {
	case <-s.done:
	case <-time.After(s.probe):
	}
	return true
}

func (s *slowProbeSensor) Start(ctx context.Context, _ chan<- monitor.Alert) error {
	<-ctx.Done()
	return nil
}

func (s *slowProbeSensor) Stop() error { return nil }

// Pairing must not wait on a sensor probe.
//
// The reply to a pairing key carries the sensor list, and building that list
// asks every sensor whether it can work here. On Windows the lid sensor answers
// that by starting PowerShell and querying WMI, under a twenty-second budget
// chosen on the grounds that the question is asked once per run and a wrong
// answer is worse than a slow one.
//
// Nothing reconciled that budget with the phone, which gives up on a pairing
// reply after ten. So when the first caller to ask happened to be a pairing
// client — a race with the background probe main starts at startup — the phone
// waited on WMI and then gave up, on a laptop that was working perfectly. It is
// the shape of an intermittent CI failure on the Windows runner, and it is also
// what a real phone meets when it pairs in the first seconds after a cold start.
//
// A sensor whose availability is not yet known is reported as not available,
// which the next status broadcast corrects. Under-reporting coverage for a
// moment is the safe direction; making the owner's phone wait is not.
func TestPairingDoesNotWaitOnASensorProbe(t *testing.T) {
	const probe = 3 * time.Second

	sensorMgr := monitor.NewManager()
	slow := newSlowProbeSensor(probe)
	sensorMgr.Register(slow)
	t.Cleanup(func() { close(slow.done) })

	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	hub := NewHub(authMgr, sensorMgr, "test")

	client := &Client{hub: hub, remoteAddr: "192.0.2.30:5000"}
	start := time.Now()
	hub.handleAuth(client, ClientMessage{Type: MsgTypeAuth, Key: authMgr.RawPairingKey()})
	elapsed := time.Since(start)

	if !client.authenticated {
		t.Fatal("the client did not pair")
	}
	// Well inside the ten seconds the phone allows, and well clear of the probe.
	if elapsed >= probe {
		t.Errorf("pairing took %v, which is the sensor probe being waited on; "+
			"the phone gives up after 10s and the probe alone is allowed 20s", elapsed)
	}
}

// The same for the status broadcast, which runs on the heartbeat every fifteen
// seconds. A probe allowed twenty would stall the beat that carries an alarm.
func TestBroadcastDoesNotWaitOnASensorProbe(t *testing.T) {
	const probe = 3 * time.Second

	sensorMgr := monitor.NewManager()
	slow := newSlowProbeSensor(probe)
	sensorMgr.Register(slow)
	t.Cleanup(func() { close(slow.done) })

	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	hub := NewHub(authMgr, sensorMgr, "test")

	start := time.Now()
	hub.broadcastStatus()
	if elapsed := time.Since(start); elapsed >= probe {
		t.Errorf("the status broadcast took %v, waiting on the sensor probe", elapsed)
	}
}
