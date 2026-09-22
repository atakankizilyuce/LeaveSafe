package ws

import (
	"encoding/json"
	"sync"
	"testing"
)

// The acceptance is the last frame either end writes in the clear. Everything
// after it is sealed — and "after it" has to mean on the wire, not in the
// source.
//
// It did not. handleAuth wrote the acceptance and installed the session on the
// next line, and an alarm, a status or an alarm_cleared noticed by any other
// goroutine in between went out unencrypted on a connection both ends had just
// agreed to seal. The window was a few microseconds wide and it was the exact
// downgrade the whole handshake exists to make impossible: anything on the
// network could read that frame, and neither end would ever know one had been
// written in the clear.
//
// What makes it testable is that the window is a race rather than a state:
// hold the write lock across both halves and there is no instant in which a
// concurrent sender can find the connection accepted and not yet sealed.
func TestNothingGoesOutInTheClearAfterTheAcceptance(t *testing.T) {
	server, _ := pairOfSessions(t)

	rec := &recorder{}
	client := &Client{transport: rec}
	rec.watching(client)

	// Twenty senders, all of them the kind of thing that reaches a client from
	// a goroutine that is not the one handling its socket: an alarm the sensor
	// manager noticed, a status broadcast, a dismissal from the terminal.
	var running sync.WaitGroup
	start := make(chan struct{})
	for range 20 {
		running.Add(1)
		go func() {
			defer running.Done()
			<-start
			client.send(ServerMessage{Type: MsgTypeAlarmCleared})
		}()
	}

	close(start)
	client.acceptAndSeal(ServerMessage{Type: MsgTypeAuthOK, Encrypt: encChaCha}, server)
	running.Wait()

	rec.mu.Lock()
	defer rec.mu.Unlock()

	accepted := false
	for i, raw := range rec.rawSent {
		var msg ServerMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("frame %d is not a message at all: %v", i, err)
		}
		if msg.Type == MsgTypeAuthOK {
			accepted = true
			continue
		}
		if !accepted {
			// Before the acceptance, in the clear is correct — the app has no
			// keys yet and could not open anything.
			continue
		}
		if msg.Type != sealedType {
			t.Fatalf("frame %d went out as %q in the clear, after the acceptance said the connection was sealed",
				i, msg.Type)
		}
	}

	if !accepted {
		t.Fatal("the acceptance never went out")
	}
}

// And the other half of the same rule, which the obvious fix would have broken:
// sealing first and sending the acceptance afterwards closes the clear-text
// window by opening a worse one, where a racing broadcast reaches the app as a
// sealed frame *before* the acceptance that tells it a seal was agreed. The app
// reads that as a handshake message, it does not parse, and the pairing fails
// for a reason nothing on either end could explain.
func TestTheAcceptanceIsTheFirstThingTheAppSees(t *testing.T) {
	server, _ := pairOfSessions(t)

	rec := &recorder{}
	client := &Client{transport: rec}
	rec.watching(client)

	var running sync.WaitGroup
	start := make(chan struct{})
	for range 20 {
		running.Add(1)
		go func() {
			defer running.Done()
			<-start
			client.send(ServerMessage{Type: MsgTypeAlarmCleared})
		}()
	}

	close(start)
	client.acceptAndSeal(ServerMessage{Type: MsgTypeAuthOK, Encrypt: encChaCha}, server)
	running.Wait()

	rec.mu.Lock()
	defer rec.mu.Unlock()

	for i, raw := range rec.rawSent {
		var msg ServerMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("frame %d is not a message at all: %v", i, err)
		}
		if msg.Type == sealedType {
			t.Fatalf("frame %d is sealed and arrived before the acceptance did", i)
		}
		if msg.Type == MsgTypeAuthOK {
			return
		}
	}
	t.Fatal("the acceptance never went out")
}
