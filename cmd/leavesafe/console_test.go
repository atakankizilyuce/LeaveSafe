package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/eventlog"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/ws"
)

// consoleFixture drives the command loop the way a person at the keyboard does
// and hands back everything it printed.
//
// The status bar is put in headless mode on purpose: that path writes through
// the logger instead of drawing a terminal grid, so the output can be captured
// without a tty and the assertions stay about content rather than layout.
func consoleFixture(t *testing.T, typed string, hub *ws.Hub) string {
	t.Helper()

	var out bytes.Buffer
	previous := log.StandardLogger().Out
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(previous) })

	runConsole(context.Background(), strings.NewReader(typed), consoleDeps{
		hub: hub,
		sb:  &statusBar{headless: true},
	})
	return out.String()
}

func testHub(t *testing.T) *ws.Hub {
	t.Helper()
	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	return ws.NewHub(authMgr, monitor.NewManager(), "test")
}

func TestClockTimeWritesTheTimeOfDayOnly(t *testing.T) {
	got := clockTime(time.Date(2026, 8, 5, 14, 3, 9, 0, time.UTC))
	if got != "14:03:09" {
		t.Errorf("clockTime = %q, want %q", got, "14:03:09")
	}
}

func TestConsoleLogFormatterStampsTheTimeOfDay(t *testing.T) {
	f := consoleLogFormatter()
	if f.TimestampFormat != clockFormat {
		t.Errorf("TimestampFormat = %q, want %q", f.TimestampFormat, clockFormat)
	}
	if !f.FullTimestamp {
		t.Error("FullTimestamp is off, so the log lines would carry no clock at all")
	}
}

// A second "arm" must report when the first one took effect rather than
// silently arming again, so the operator can tell the two situations apart.
func TestConsoleArmTwiceReportsWhenItWasArmed(t *testing.T) {
	hub := testHub(t)
	out := consoleFixture(t, "arm\narm\n", hub)

	if !strings.Contains(out, "Armed from console") {
		t.Fatalf("the first arm did not take effect; output was:\n%s", out)
	}
	want := "Already armed since " + clockTime(hub.ArmedAt())
	if !strings.Contains(out, want) {
		t.Errorf("second arm did not name the time it was armed, want %q in:\n%s", want, out)
	}
}

func TestConsoleStatusNamesWhenItWasArmed(t *testing.T) {
	hub := testHub(t)
	out := consoleFixture(t, "arm\nstatus\n", hub)

	if !strings.Contains(out, "ARMED") {
		t.Fatalf("status did not report the armed state; output was:\n%s", out)
	}
	if want := "since " + clockTime(hub.ArmedAt()); !strings.Contains(out, want) {
		t.Errorf("status did not name the arming time, want %q in:\n%s", want, out)
	}
}

func TestConsoleStatusOnAnIdleMonitorSaysDisarmed(t *testing.T) {
	out := consoleFixture(t, "status\n", testHub(t))
	if !strings.Contains(out, "DISARMED") {
		t.Errorf("an unarmed monitor did not report itself disarmed; output was:\n%s", out)
	}
}

// writeEventLog points the config directory at a temporary one and puts a
// history in it. Both environment variables are set because ConfigDir reads
// APPDATA on Windows and the home directory everywhere else.
func writeEventLog(t *testing.T, events []eventlog.Event) {
	t.Helper()

	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	dir := config.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}

	var body strings.Builder
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	path := filepath.Join(dir, eventLogFileName)
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatalf("write event log: %v", err)
	}
}

func TestConsoleHistoryRendersEachEventWithItsTime(t *testing.T) {
	armed := time.Date(2026, 8, 5, 9, 15, 0, 0, time.UTC)
	tripped := time.Date(2026, 8, 5, 9, 41, 30, 0, time.UTC)
	writeEventLog(t, []eventlog.Event{
		{Timestamp: armed, Type: eventlog.EventArm, Message: "Armed from console"},
		{Timestamp: tripped, Type: eventlog.EventAlert, Sensor: "power", Message: "Charger unplugged"},
	})

	out := consoleFixture(t, "history\n", testHub(t))

	// The entry with a sensor and the one without take different branches, and
	// both have to carry the time the event actually happened.
	if want := clockTime(armed) + " [arm] Armed from console"; !strings.Contains(out, want) {
		t.Errorf("history missed the sensorless entry, want %q in:\n%s", want, out)
	}
	if want := clockTime(tripped) + " [alert] power"; !strings.Contains(out, want) {
		t.Errorf("history missed the sensor entry, want %q in:\n%s", want, out)
	}
}

func TestConsoleHistoryWithNothingRecordedSaysSo(t *testing.T) {
	writeEventLog(t, nil)

	out := consoleFixture(t, "history\n", testHub(t))
	if !strings.Contains(out, "No events recorded yet") {
		t.Errorf("an empty history was not reported as empty; output was:\n%s", out)
	}
}
