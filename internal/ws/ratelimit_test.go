package ws

import (
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/monitor"
)

// fixedClock returns a clock the test moves by hand, so the refill can be
// exercised without sleeping through it.
func fixedClock(start time.Time) (func() time.Time, func(time.Duration)) {
	now := start
	return func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }
}

// The burst is what a person is allowed; past it the flood stops.
func TestBucketAllowsTheBurstThenStops(t *testing.T) {
	clock, _ := fixedClock(time.Unix(1000, 0))
	b := newTokenBucket()
	b.now = clock

	for i := range messageBurst {
		if !b.allow() {
			t.Fatalf("message %d was refused inside the burst", i)
		}
	}
	if b.allow() {
		t.Error("a message past the burst was allowed")
	}
}

// The allowance comes back with time, or one burst would silence a client for
// the rest of its connection.
func TestBucketRefillsOverTime(t *testing.T) {
	clock, advance := fixedClock(time.Unix(1000, 0))
	b := newTokenBucket()
	b.now = clock

	for range messageBurst {
		b.allow()
	}
	if b.allow() {
		t.Fatal("the bucket was not empty to begin with")
	}

	advance(time.Second) // eight per second
	allowed := 0
	for range 20 {
		if b.allow() {
			allowed++
		}
	}
	if allowed != messagesPerSecond {
		t.Errorf("a second of refill allowed %d messages, want %d", allowed, messagesPerSecond)
	}
}

// Refill must not accumulate past the burst, or an idle client would come back
// holding an unbounded allowance.
func TestBucketDoesNotRefillPastTheBurst(t *testing.T) {
	clock, advance := fixedClock(time.Unix(1000, 0))
	b := newTokenBucket()
	b.now = clock

	advance(time.Hour)

	allowed := 0
	for range 100 {
		if b.allow() {
			allowed++
		}
	}
	if allowed != messageBurst {
		t.Errorf("an hour idle allowed %d messages, want the burst of %d", allowed, messageBurst)
	}
}

// What the limit is actually for: update_config writes config.json on every
// message, and a client holding a session can send it without the PIN.
func TestFloodOfConfigWritesIsCutOff(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	hub := testHub(t)
	hub.sensorMgr.Register(monitor.NewPowerSensor())
	hub.SetConfig(config.Default())
	client := pairedClient(t, hub)

	// The bucket refills from the clock, and every write the limiter allows
	// touches config.json. On a slow disk the loop below runs long enough to
	// refill it several times over, so with the real clock the count is a
	// measure of the filesystem rather than of the limit. Hand the client a
	// bucket that is standing still, and the flood is cut off at the burst
	// wherever the test runs.
	client.limiter = newTokenBucket()
	client.limiter.now, _ = fixedClock(time.Unix(1000, 0))

	payload := configToPayload(config.Default())
	handled := 0
	for range 500 {
		before := hub.cfg.HeartbeatSeconds
		payload.HeartbeatSeconds = before + 1
		hub.handleMessage(client, ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload})
		if hub.cfg.HeartbeatSeconds != before {
			handled++
		}
	}

	if handled == 0 {
		t.Fatal("the limiter refused everything; a real phone could not save a setting")
	}
	if handled >= 500 {
		t.Errorf("all %d config writes went through; the limiter did nothing", handled)
	}
	if handled != messageBurst {
		t.Errorf("%d config writes went through, want the burst of %d",
			handled, messageBurst)
	}
}

// A phone sends a ping every fifteen seconds and a message when a thumb moves.
// The limit has to be invisible at that rate, or it has replaced one failure
// with another.
func TestOrdinaryPhoneTrafficIsNeverLimited(t *testing.T) {
	clock, advance := fixedClock(time.Unix(1000, 0))
	b := newTokenBucket()
	b.now = clock

	// Two minutes of a ping every fifteen seconds, with a burst of taps in
	// between — arming, opening settings, dismissing something.
	for range 8 {
		if !b.allow() {
			t.Fatal("a heartbeat ping was refused")
		}
		for range 5 {
			advance(300 * time.Millisecond)
			if !b.allow() {
				t.Fatal("a tap was refused at human speed")
			}
		}
		advance(15 * time.Second)
	}
}
