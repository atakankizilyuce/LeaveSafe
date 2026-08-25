package main

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/monitor"
)

// The dashboard takes the keyboard: raw mode, no echo, and none of the
// keystrokes the terminal used to turn into signals on our behalf. That is a
// bargain, and this file is the half of it the program owes back. Whatever else
// it is doing, Ctrl+C has to work — it is the only way out of a screen that has
// taken the terminal over, and a user who cannot take it is left killing the
// process from another window and inheriting a terminal still in raw mode.
//
// It stopped working once already. Reading the keyboard and running the command
// were one goroutine, so for as long as a command took, nothing was reading:
// an `arm` that waited on six sensors being asked whether they work was a
// minute of a terminal that answered nothing at all.

func TestCtrlCIsAnsweredWhileACommandIsStillRunning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)

	previous := consoleCommands["help"]
	consoleCommands["help"] = consoleCommand{run: func(context.Context, *console) {
		close(started)
		<-release
	}}
	t.Cleanup(func() { consoleCommands["help"] = previous })

	sb := &statusBar{out: io.Discard, hub: testHub(t), sensorMgr: monitor.NewManager(), line: &inputLine{}}
	sb.paint()

	quit := make(chan struct{})
	// `help` starts a command that will not come back; the Ctrl+C behind it is
	// what a person types once they realize that.
	go runConsole(context.Background(), strings.NewReader("help\r\x03"), consoleDeps{
		sb:   sb,
		quit: func() { close(quit) },
	})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the slow command never started")
	}

	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Fatal("Ctrl+C went unanswered while a command was running: the terminal is " +
			"in raw mode, so there is no other way out of it")
	}
}

// The other half of the same bargain: a keystroke is drawn by whoever consumes
// it, not by the goroutine that reads it. Read ahead and drawn on arrival, a PIN
// typed before its prompt appeared would go on screen in the clear — on the
// screen of the laptop somebody has just picked up.
func TestAPinTypedAheadOfItsPromptIsStillMasked(t *testing.T) {
	screen := &syncBuffer{}
	sb := &statusBar{out: screen, hub: testHub(t), sensorMgr: monitor.NewManager(), line: &inputLine{}}
	sb.paint()

	// Both lines are typed in one go, before anything has read either: the
	// command, and the PIN behind it.
	reader := &typedLines{
		in:   bufio.NewReader(strings.NewReader("disarm\r2468\r")),
		sb:   sb,
		quit: func() {},
	}

	if line, ok := reader.readLine(); !ok || line != "disarm" {
		t.Fatalf("read %q (ok %v), want the command", line, ok)
	}
	// Whatever the pump has read by now must not be on screen yet.
	if strings.Contains(screen.String(), "2468") {
		t.Fatalf("the PIN was drawn before anything asked for it; screen was:\n%q", screen.String())
	}

	pin, ok := reader.readSecret()
	if !ok || pin != "2468" {
		t.Fatalf("read %q (ok %v), want the PIN", pin, ok)
	}
	if strings.Contains(screen.String(), "2468") {
		t.Errorf("the PIN reached the screen; screen was:\n%q", screen.String())
	}
}
