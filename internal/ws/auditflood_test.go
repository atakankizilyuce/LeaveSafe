package ws

import (
	"path/filepath"
	"testing"

	"github.com/leavesafe/leavesafe/internal/eventlog"
)

// hubWithEventLog returns a hub writing its security events to a file the test
// can read back.
func hubWithEventLog(t *testing.T) (*Hub, string) {
	t.Helper()
	hub := testHub(t)
	path := filepath.Join(t.TempDir(), "events.jsonl")
	el, err := eventlog.New(path)
	if err != nil {
		t.Fatalf("event log: %v", err)
	}
	t.Cleanup(func() { _ = el.Close() })
	hub.SetEventLogger(el)
	return hub, path
}

// countEvents returns how many records of a given type are in the log.
func countEvents(t *testing.T, path string, kind eventlog.EventType) int {
	t.Helper()
	events, err := eventlog.ReadLast(path, 100000)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	n := 0
	for _, ev := range events {
		if ev.Type == kind {
			n++
		}
	}
	return n
}

// The security event log is the record of what happened while the owner was
// away, and it is size-rotated — roughly six megabytes across its generations.
// A refused pairing attempt used to write a line to it, with no limit on how
// fast attempts could arrive and no session required to send them. So an
// unauthenticated peer could push every genuine record out of the file: the
// arm, the sensor alert that fired when they picked the laptop up, the
// disconnect. Erasing the evidence needed no key.
//
// Two things now bound it. Attempts are metered per socket, and an attempt made
// against an address already serving a lockout writes nothing, because the
// lockout was recorded when it started.
func TestAPairingFloodCannotEraseTheEventLog(t *testing.T) {
	hub, path := hubWithEventLog(t)
	client := &Client{hub: hub, remoteAddr: "192.0.2.66:5000"}

	const attempts = 5000
	for range attempts {
		hub.handleMessage(client, ClientMessage{Type: MsgTypeAuth, Key: "0000000000000000"})
	}

	written := countEvents(t, path, eventlog.EventAuthFail)
	if written == 0 {
		t.Fatal("nothing was recorded; a refused pairing attempt must still be auditable")
	}
	// The genuine failures before the lockout are worth recording. Thousands of
	// attempts behind it are not, and the whole log holds far fewer records than
	// this flood would have written.
	if written > hub.authManager.MaxAttempts()+authAttemptMargin {
		t.Errorf("%d of %d refused attempts were written to the audit log; "+
			"that is enough to rotate genuine history out of it", written, attempts)
	}
}

// The suppression must not hide the lockout itself, or the log stops answering
// "was someone guessing at this?" — which is the question it exists for.
func TestTheLockoutItselfIsStillRecorded(t *testing.T) {
	hub, path := hubWithEventLog(t)
	client := &Client{hub: hub, remoteAddr: "192.0.2.67:5000"}

	for range hub.authManager.MaxAttempts() {
		hub.handleMessage(client, ClientMessage{Type: MsgTypeAuth, Key: "0000000000000000"})
	}

	if got := countEvents(t, path, eventlog.EventAuthFail); got < hub.authManager.MaxAttempts() {
		t.Errorf("only %d of %d genuine failed attempts were recorded",
			got, hub.authManager.MaxAttempts())
	}
}

// Metering pairing must not cost the honest case: a phone sends one attempt per
// connection, and a second when the user mistypes the key.
func TestAPhoneCanStillMistypeTheKey(t *testing.T) {
	hub := testHub(t)
	client := &Client{hub: hub, remoteAddr: "192.0.2.68:5000"}

	for i := range 3 {
		hub.handleMessage(client, ClientMessage{Type: MsgTypeAuth, Key: "0000000000000000"})
		if client.authenticated {
			t.Fatalf("attempt %d paired with a wrong key", i)
		}
	}
	hub.handleMessage(client, ClientMessage{Type: MsgTypeAuth, Key: hub.authManager.RawPairingKey()})
	if !client.authenticated {
		t.Error("a phone that mistyped the key three times could not then pair correctly")
	}
}
