package main

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/alarm"
	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/remote"
	"github.com/leavesafe/leavesafe/internal/ws"
)

// The command loop is the whole of the local interface: with no phone paired and
// no dashboard to click, typing at it is the only way to arm the machine, disarm
// it, rotate a key that has been shared too widely, or find out why nothing
// happened last Tuesday.
//
// These tests are about how a typed line is turned into a command at all — which
// words are answered to, which are not, and what a command does with the rest of
// the line. The commands themselves are exercised alongside, because the loop and
// the command are the same thing to whoever is typing.

// quoted is a value as it appears in captured log output. The console quotes
// what the user typed, and the logger escapes those quotes on its way out, so an
// assertion written with plain ones would never match.
func quoted(s string) string {
	return `\"` + s + `\"`
}

// tempConfigDir points config.ConfigDir at a directory of this test's own, so a
// command that saves a setting cannot reach the config of whoever is running the
// suite. Both variables are set because ConfigDir reads APPDATA on Windows and
// the home directory everywhere else.
func tempConfigDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

// unwritableConfigDir points the config directory at a plain file, so that every
// attempt to save fails. That is the only honest way to reach the branches that
// report a setting the program could not write down.
func unwritableConfigDir(t *testing.T) {
	t.Helper()

	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("in the way"), 0o600); err != nil {
		t.Fatalf("write the blocking file: %v", err)
	}
	t.Setenv("APPDATA", blocked)
	t.Setenv("HOME", blocked)
	t.Setenv("USERPROFILE", blocked)
}

// hubWithSensors is a hub that has the real sensor set registered, which is what
// the commands that name sensors need to have anything to name.
func hubWithSensors(t *testing.T) *ws.Hub {
	t.Helper()

	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	mgr := monitor.NewManager()
	registerSensors(mgr, config.Default())
	return ws.NewHub(authMgr, mgr, "test")
}

// quietAlarm reports itself sounding while making no sound and writing no log.
//
// Both halves are deliberate. Escalation rather than a plain siren keeps every
// audio backend out of it — a test suite that shrieks at full volume is not one
// anybody runs twice. The hour-long delay on its only step keeps the escalation
// goroutine parked on the stop channel, so it never logs: these tests read the
// log through a buffer, and a background goroutine writing to it while the test
// reads is a data race, which is exactly what -race found the first time round.
func quietAlarm(t *testing.T) *alarm.Alarm {
	t.Helper()

	a := alarm.New(config.AlarmConfig{
		EscalationEnabled: true,
		Levels:            []config.AlarmLevel{{DelaySeconds: 3600, Action: "notify_phone_only"}},
	})
	a.Start()
	t.Cleanup(a.Stop)
	return a
}

// ---- what counts as a command -------------------------------------------

func TestConsoleReportsAWordItDoesNotKnow(t *testing.T) {
	out := consoleFixture(t, "wibble\n", testHub(t))

	if !strings.Contains(out, "Unknown command: "+quoted("wibble")) {
		t.Errorf("an unknown word was not reported; output was:\n%s", out)
	}
}

// "arm now" is not an instruction to arm. Taking the first word and ignoring the
// rest would turn a typo into an action on the machine the user is guarding.
func TestConsoleRefusesACommandGivenAnArgumentItDoesNotTake(t *testing.T) {
	hub := testHub(t)

	out := consoleFixture(t, "arm now\n", hub)

	if !strings.Contains(out, "Unknown command: "+quoted("arm now")) {
		t.Errorf("a command with a stray argument was accepted; output was:\n%s", out)
	}
	if hub.IsArmed() {
		t.Error("the machine was armed by a line that was not the arm command")
	}
}

// The other way round: "qr" on its own is not this command with its number left
// off, it is a word the console does not know. Answering it with "No URL 0"
// would send the user looking for a list entry that was never the problem.
func TestConsoleRefusesACommandMissingItsArgument(t *testing.T) {
	for _, typed := range []string{"qr", "trigger"} {
		out := consoleFixture(t, typed+"\n", testHub(t))

		if !strings.Contains(out, "Unknown command: "+quoted(typed)) {
			t.Errorf("%q was not reported as unknown; output was:\n%s", typed, out)
		}
	}
}

func TestConsoleIgnoresBlankLines(t *testing.T) {
	out := consoleFixture(t, "\n   \n\t\n", testHub(t))

	if strings.Contains(out, "Unknown command") {
		t.Errorf("a blank line was answered as a command; output was:\n%s", out)
	}
}

// The help line is written by hand, so nothing but a test keeps it in step with
// the commands the loop actually answers to. A command missing from it is one
// the user has no way to discover.
func TestConsoleHelpNamesEveryCommandTheLoopAnswersTo(t *testing.T) {
	out := consoleFixture(t, "help\n", testHub(t))

	for name := range consoleCommands {
		// silence is a deliberate alias for stop and is not advertised: one name
		// per thing is what makes the list readable.
		if name == "silence" {
			continue
		}
		if !strings.Contains(out, name) {
			t.Errorf("help does not name the %q command; output was:\n%s", name, out)
		}
	}
}

// ---- the commands --------------------------------------------------------

func TestConsoleTestSendsAnAlertAndSaysWhoGotIt(t *testing.T) {
	out := consoleFixture(t, "test\n", testHub(t))

	if !strings.Contains(out, "Alert sent to 0 client(s)") {
		t.Errorf("test did not report where the alert went; output was:\n%s", out)
	}
}

func TestConsoleTriggerFiresTheSensorItNames(t *testing.T) {
	out := consoleFixture(t, "trigger power\n", hubWithSensors(t))

	if !strings.Contains(out, "Sensor "+quoted("power")+" triggered") {
		t.Errorf("trigger did not fire the sensor; output was:\n%s", out)
	}
}

// A sensor this build does not have is a typo, not an alarm. Firing something
// else instead would be the worst possible reading of it.
func TestConsoleTriggerSaysWhenThereIsNoSuchSensor(t *testing.T) {
	out := consoleFixture(t, "trigger barometer\n", hubWithSensors(t))

	if !strings.Contains(out, "Unknown sensor: "+quoted("barometer")) {
		t.Errorf("trigger accepted a sensor that does not exist; output was:\n%s", out)
	}
}

func TestConsoleStopWithNothingSoundingSaysSo(t *testing.T) {
	out := consoleOutput(t, "stop\n", consoleDeps{
		hub:        testHub(t),
		sb:         &statusBar{headless: true},
		localAlarm: alarm.New(config.AlarmConfig{}),
	})

	if !strings.Contains(out, "No alarm is currently active") {
		t.Errorf("stop invented an alarm to dismiss; output was:\n%s", out)
	}
}

// Both words dismiss it. "silence" exists because it is what people type at a
// noise, and having it fall through to "unknown command" while the machine is
// shrieking would be the worst moment to be pedantic about vocabulary.
func TestConsoleStopAndSilenceBothDismissASoundingAlarm(t *testing.T) {
	for _, typed := range []string{"stop", "silence"} {
		out := consoleOutput(t, typed+"\n", consoleDeps{
			hub:        testHub(t),
			sb:         &statusBar{headless: true},
			localAlarm: quietAlarm(t),
		})

		if !strings.Contains(out, "Alarm dismissed from console") {
			t.Errorf("%q did not dismiss the alarm; output was:\n%s", typed, out)
		}
	}
}

func TestConsoleDisarmOnAnIdleMonitorSaysSo(t *testing.T) {
	out := consoleFixture(t, "disarm\n", testHub(t))

	if !strings.Contains(out, "Already disarmed") {
		t.Errorf("disarm on an idle monitor did not say so; output was:\n%s", out)
	}
}

func TestConsoleDisarmWithNoPinSetJustDisarms(t *testing.T) {
	hub := testHub(t)

	out := consoleFixture(t, "arm\ndisarm\n", hub)

	if !strings.Contains(out, "Disarmed from console") {
		t.Errorf("disarm did not take effect; output was:\n%s", out)
	}
	if hub.IsArmed() {
		t.Error("the machine is still armed after disarm")
	}
}

// The PIN is what stands between whoever is holding the laptop and the alarm it
// is sounding. A wrong one has to leave the machine armed.
func TestConsoleDisarmRefusesTheWrongPinAndStaysArmed(t *testing.T) {
	hub := testHub(t)
	hash, err := auth.HashPin("2468")
	if err != nil {
		t.Fatalf("hash the PIN: %v", err)
	}
	hub.SetPinProtection(true, hash)

	out := consoleFixture(t, "arm\ndisarm\n1111\n", hub)

	if !strings.Contains(out, "Refused") {
		t.Fatalf("the wrong PIN was not refused; output was:\n%s", out)
	}
	if !hub.IsArmed() {
		t.Error("the machine was disarmed by a PIN that was refused")
	}
}

func TestConsoleDisarmAcceptsTheRightPin(t *testing.T) {
	hub := testHub(t)
	hash, err := auth.HashPin("2468")
	if err != nil {
		t.Fatalf("hash the PIN: %v", err)
	}
	hub.SetPinProtection(true, hash)

	out := consoleFixture(t, "arm\ndisarm\n2468\n", hub)

	if !strings.Contains(out, "PIN required to disarm") {
		t.Fatalf("disarm did not ask for the PIN; output was:\n%s", out)
	}
	if !strings.Contains(out, "Disarmed from console") {
		t.Fatalf("the right PIN did not disarm; output was:\n%s", out)
	}
	if hub.IsArmed() {
		t.Error("the machine is still armed after the right PIN")
	}
}

// Input that stops between the question and the answer — a closed terminal, a
// killed ssh session — must leave the machine as it was rather than treating
// nothing as an answer.
func TestConsoleDisarmLeavesTheMachineArmedWhenTheInputStops(t *testing.T) {
	hub := testHub(t)
	hash, err := auth.HashPin("2468")
	if err != nil {
		t.Fatalf("hash the PIN: %v", err)
	}
	hub.SetPinProtection(true, hash)

	out := consoleFixture(t, "arm\ndisarm\n", hub)

	if !strings.Contains(out, "PIN required to disarm") {
		t.Fatalf("disarm did not ask for the PIN; output was:\n%s", out)
	}
	if strings.Contains(out, "Disarmed from console") {
		t.Error("the machine was disarmed without a PIN being typed")
	}
	if !hub.IsArmed() {
		t.Error("the machine is no longer armed after an unanswered PIN prompt")
	}
}

func TestConsoleStatusListsEverySensorWithItsState(t *testing.T) {
	hub := hubWithSensors(t)

	out := consoleFixture(t, "status\n", hub)

	if !strings.Contains(out, "Phones connected: 0") {
		t.Errorf("status did not report the connection count; output was:\n%s", out)
	}
	for _, s := range hub.GetSensorInfos() {
		if !strings.Contains(out, s.Name) {
			t.Errorf("status did not list the %q sensor; output was:\n%s", s.Name, out)
		}
	}
}

// The word beside a sensor's name is the whole of what the user reads before
// walking away from the laptop. A failed sensor shown as "on" is the one version
// of this that gets somebody's laptop stolen.
func TestSensorMarkNamesTheStateTheUserNeedsToSee(t *testing.T) {
	cases := map[string]struct {
		info ws.SensorInfo
		want string
	}{
		"a machine without one":  {ws.SensorInfo{Available: false, Enabled: true}, "unavailable"},
		"a driver that failed":   {ws.SensorInfo{Available: true, Enabled: true, Failure: "device gone"}, "FAILED — device gone"},
		"watching":               {ws.SensorInfo{Available: true, Enabled: true}, "on"},
		"switched off by choice": {ws.SensorInfo{Available: true, Enabled: false}, "off"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := sensorMark(tc.info); got != tc.want {
				t.Errorf("sensorMark = %q, want %q", got, tc.want)
			}
		})
	}
}

// A history the program cannot read is a different situation from one with
// nothing in it, and the two have to be told apart: the first is a fault, the
// second is a quiet week.
func TestConsoleHistoryWithNoLogFileSaysItCannotBeRead(t *testing.T) {
	tempConfigDir(t)

	out := consoleFixture(t, "history\n", testHub(t))

	if !strings.Contains(out, "No event history available") {
		t.Errorf("a missing history was not reported; output was:\n%s", out)
	}
}

func TestConsoleQRRejectsSomethingThatIsNotANumber(t *testing.T) {
	sb := dashboardWith([]string{"http://192.168.1.10:8080"}, "", "")

	out := consoleOutput(t, "qr banana\n", consoleDeps{hub: testHub(t), sb: sb})

	if !strings.Contains(out, "Usage: qr <n>") {
		t.Errorf("qr did not say how it is used; output was:\n%s", out)
	}
}

func TestConsoleQRSaysWhenThereIsNoSuchAddress(t *testing.T) {
	sb := dashboardWith([]string{"http://192.168.1.10:8080"}, "", "")

	out := consoleOutput(t, "qr 9\n", consoleDeps{hub: testHub(t), sb: sb})

	if !strings.Contains(out, "No URL 9") {
		t.Errorf("qr accepted an address that is not in the list; output was:\n%s", out)
	}
}

// terminalDashboard is a status bar in its drawing mode, writing to a buffer
// instead of a terminal. Everything about `qr <n>` lives on that side of the
// headless switch — a run with no terminal has no QR code to switch — so this is
// the only way to reach it at all.
func terminalDashboard(t *testing.T, urls []string) (*statusBar, *syncBuffer) {
	t.Helper()

	screen := &syncBuffer{}
	sb := &statusBar{
		out:       screen,
		hub:       testHub(t),
		sensorMgr: monitor.NewManager(),
		key:       testKey,
		rawKey:    testRawKey,
		// A QR box with height, or drawQR draws nothing and the selection is
		// never read back.
		gridRow: 1, gridCol: 1, gridWidth: 78,
		qrRow: 1, qrCol: 1, qrBoxW: 40, qrBoxH: 40,
	}
	sb.setURLs(urls)
	return sb, screen
}

func TestConsoleQRNamesTheAddressItIsNowShowing(t *testing.T) {
	const url = "http://192.168.1.10:8080"
	sb, screen := terminalDashboard(t, []string{url, "https://198.51.100.4:9443"})

	runConsole(context.Background(), strings.NewReader("qr 2\n"), consoleDeps{hub: sb.hub, sb: sb})

	if !strings.Contains(screen.String(), "Now showing https://198.51.100.4:9443") {
		t.Fatalf("qr did not name what it switched to; screen was:\n%s", screen.String())
	}
	// The number the user typed has to be the one the dashboard is now drawing,
	// or the code on screen and the address in the reply disagree.
	if _, selected := sb.urlListWithSelection(); selected != 1 {
		t.Errorf("the dashboard is showing URL %d, want the second one", selected)
	}
}

// The list is what `qr <n>` takes its numbers from, so it also has to say which
// of them is on screen right now. Without the marker the user has no way to tell
// whether the code they are looking at is the one they want.
func TestConsoleUrlsMarksTheOneTheQRCodeIsShowing(t *testing.T) {
	sb, screen := terminalDashboard(t, []string{"http://192.168.1.10:8080", "https://198.51.100.4:9443"})

	runConsole(context.Background(), strings.NewReader("qr 2\nurls\n"), consoleDeps{hub: sb.hub, sb: sb})

	if !strings.Contains(screen.String(), "[2]* https://198.51.100.4:9443") {
		t.Errorf("urls did not mark the address on screen; screen was:\n%s", screen.String())
	}
}

// Rotating the key invalidates the old one whether or not the file can be
// rewritten, so a write that fails has to be said out loud: the next start would
// otherwise come back with a key the user believes they revoked.
func TestConsoleRotateKeyReportsAStoredKeyItCouldNotUpdate(t *testing.T) {
	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	// A path whose parent is a plain file. Saving creates the directory it needs,
	// so an absent one is not enough — something has to be in the way.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("in the way"), 0o600); err != nil {
		t.Fatalf("write the blocking file: %v", err)
	}
	keyPath := filepath.Join(blocked, "pairing.key")
	sb := dashboardWith([]string{"http://192.168.1.10:8080"}, "", keyPath)

	out := consoleOutput(t, "rotate-key\n", consoleDeps{
		hub:     testHub(t),
		sb:      sb,
		authMgr: authMgr,
	})

	if !strings.Contains(out, "the stored key could not be updated") {
		t.Fatalf("rotate-key hid a failed write; output was:\n%s", out)
	}
	// The rotation itself still happened, and saying so is the point: the old
	// key no longer opens anything.
	if !strings.Contains(out, "Pairing key rotated") {
		t.Errorf("rotate-key did not report the rotation it did perform; output was:\n%s", out)
	}
}

// A hub with no config checks for updates, so the branch that says checking is
// off is only reached with a config that switches it off — which is also the
// branch that must not touch the network.
func TestConsoleUpdateSaysWhenCheckingIsSwitchedOff(t *testing.T) {
	hub := testHub(t)
	cfg := config.Default()
	off := false
	cfg.UpdateCheck = &off
	hub.SetConfig(cfg)

	out := consoleFixture(t, "update\n", hub)

	if !strings.Contains(out, "Update checking is off") {
		t.Errorf("update did not report that checking is off; output was:\n%s", out)
	}
}

// ---- the two commands that ask a follow-up question ----------------------

// modeDeps is a console wired for the connection-mode command: a real listening
// server for the address list, and a controller whose certificate step fails so
// that enabling remote access reports a reason instead of reaching a router.
func modeDeps(t *testing.T, cfg *config.Config) consoleDeps {
	t.Helper()

	srv := testServer(t)
	ctl := remote.NewController(srv, config.ConfigDir(), 0, remote.Deps{
		Cert: func(string) (tls.Certificate, string, error) {
			return tls.Certificate{}, "", errors.New("no certificate on a test machine")
		},
		OpenPort: func(int) (remote.PortMapping, error) {
			return nil, errors.New("no router here")
		},
		PublicIP: func() (string, error) { return "", errors.New("offline") },
	})
	return consoleDeps{
		hub:       testHub(t),
		sb:        newHeadlessStatusBar(nil, nil, testKey, testRawKey, srv.URLs(), "", ""),
		srv:       srv,
		remoteCtl: ctl,
		cfg:       cfg,
	}
}

// standingListener is a remote listener that always comes up, so a test can put
// the controller in the state it reaches after remote access is genuinely
// running — which is the only way to see what the console says to someone who is
// already in that mode.
type standingListener struct{ port int }

func (l standingListener) StartRemote(tls.Certificate, string, int) (int, error) { return l.port, nil }
func (l standingListener) StopRemote()                                           {}

// enabledModeDeps is modeDeps with remote access already up.
//
// It waits for the reachability probe to finish before handing the console over.
// That probe runs on its own goroutine and logs what it found, and the console
// tests read the log — so letting it settle first is the difference between a
// deterministic test and one that fails on a busy machine.
func enabledModeDeps(t *testing.T, cfg *config.Config) consoleDeps {
	t.Helper()

	srv := testServer(t)
	ctl := remote.NewController(standingListener{port: 9443}, config.ConfigDir(), 9443, remote.Deps{
		Cert: func(string) (tls.Certificate, string, error) {
			return tls.Certificate{}, "AA:BB:CC:DD", nil
		},
		OpenPort: func(int) (remote.PortMapping, error) {
			return nil, errors.New("no router here")
		},
		PublicIP: func() (string, error) { return "", errors.New("offline") },
	})
	ctl.Enable(context.Background())

	deadline := time.Now().Add(5 * time.Second)
	for ctl.State().Probing {
		if time.Now().After(deadline) {
			t.Fatal("the reachability probe never finished")
		}
		time.Sleep(time.Millisecond)
	}
	if !ctl.State().Enabled {
		t.Fatal("remote access did not come up, so there is no enabled state to test against")
	}

	return consoleDeps{
		hub:       testHub(t),
		sb:        newHeadlessStatusBar(nil, nil, testKey, testRawKey, srv.URLs(), "", ""),
		srv:       srv,
		remoteCtl: ctl,
		cfg:       cfg,
	}
}

// The question has to open on the mode the machine is actually in. Offering
// "(currently 1)" to someone whose laptop is reachable from the internet would
// have them type 2 to "turn it on" and be told it is already on.
func TestConsoleModeOpensOnTheModeInForce(t *testing.T) {
	tempConfigDir(t)

	out := consoleOutput(t, "mode\n\n", enabledModeDeps(t, config.Default()))

	if !strings.Contains(out, "(currently 2)") {
		t.Errorf("mode did not open on remote access; output was:\n%s", out)
	}
}

// Switching back off takes the listener down without bringing anything up, which
// is the one path through this command that never touches Enable.
func TestConsoleModeSwitchesRemoteAccessBackOff(t *testing.T) {
	tempConfigDir(t)
	cfg := config.Default()

	out := consoleOutput(t, "mode\n1\n", enabledModeDeps(t, cfg))

	if !strings.Contains(out, "Connection mode: "+connectionModeName(false)) {
		t.Fatalf("mode did not report the switch; output was:\n%s", out)
	}
	if cfg.RemoteAccess == nil || *cfg.RemoteAccess {
		t.Error("the config still records remote access as wanted")
	}
}

func TestConsoleModeLeavesItAloneOnAnEmptyAnswer(t *testing.T) {
	tempConfigDir(t)

	out := consoleOutput(t, "mode\n\n", modeDeps(t, config.Default()))

	if !strings.Contains(out, "[1] Wi-Fi only") {
		t.Fatalf("mode did not offer the choice; output was:\n%s", out)
	}
	if !strings.Contains(out, "Left unchanged") {
		t.Errorf("an empty answer changed something; output was:\n%s", out)
	}
}

func TestConsoleModeSaysWhenItIsAlreadyInThatMode(t *testing.T) {
	tempConfigDir(t)

	out := consoleOutput(t, "mode\n1\n", modeDeps(t, config.Default()))

	if !strings.Contains(out, "Already in that mode") {
		t.Errorf("mode did not notice it was being asked for what it already does; output was:\n%s", out)
	}
}

func TestConsoleModeEndsQuietlyWhenTheInputStops(t *testing.T) {
	tempConfigDir(t)

	out := consoleOutput(t, "mode\n", modeDeps(t, config.Default()))

	if !strings.Contains(out, "Type 1 or 2") {
		t.Fatalf("mode did not ask; output was:\n%s", out)
	}
	if strings.Contains(out, "Connection mode:") {
		t.Errorf("mode acted on an answer nobody gave; output was:\n%s", out)
	}
}

// The setting is written to disk before the listener moves, so that a crash in
// between leaves the config saying what the user asked for. A write that fails
// therefore has to stop the whole thing rather than move the listener anyway.
func TestConsoleModeStopsWhenTheSettingCannotBeSaved(t *testing.T) {
	cfg := config.Default()
	deps := modeDeps(t, cfg)
	unwritableConfigDir(t)

	out := consoleOutput(t, "mode\n2\n", deps)

	if !strings.Contains(out, "Could not save the setting") {
		t.Fatalf("mode did not report the failed save; output was:\n%s", out)
	}
	if strings.Contains(out, "Connection mode:") {
		t.Error("the listener was moved after the setting could not be written down")
	}
}

// Switching on with no certificate available: the mode is recorded and the
// reason it is not actually running reaches the dashboard, rather than the user
// being told remote access is on when nothing is listening.
func TestConsoleModeSwitchingOnReportsWhatItCouldNotDo(t *testing.T) {
	tempConfigDir(t)
	cfg := config.Default()

	out := consoleOutput(t, "mode\n2\n", modeDeps(t, cfg))

	if !strings.Contains(out, "Connection mode: "+connectionModeName(true)) {
		t.Fatalf("mode did not report the switch; output was:\n%s", out)
	}
	if !strings.Contains(out, "Could not create the TLS certificate") {
		t.Errorf("the reason remote access is not running never reached the user; output was:\n%s", out)
	}
	if cfg.RemoteAccess == nil || !*cfg.RemoteAccess {
		t.Error("the config does not record what the user asked for")
	}
}

func TestConsoleLangLeavesItAloneOnAnEmptyAnswer(t *testing.T) {
	tempConfigDir(t)
	cfg := config.Default()

	out := consoleOutput(t, "lang\n\n", consoleDeps{
		hub: testHub(t), sb: &statusBar{headless: true}, cfg: cfg,
	})

	if !strings.Contains(out, "Left unchanged") {
		t.Errorf("an empty answer changed the language; output was:\n%s", out)
	}
}

func TestConsoleLangEndsQuietlyWhenTheInputStops(t *testing.T) {
	tempConfigDir(t)

	out := consoleOutput(t, "lang\n", consoleDeps{
		hub: testHub(t), sb: &statusBar{headless: true}, cfg: config.Default(),
	})

	if !strings.Contains(out, "Type 1 or 2") {
		t.Fatalf("lang did not ask; output was:\n%s", out)
	}
	if strings.Contains(out, "from the next start") {
		t.Errorf("lang acted on an answer nobody gave; output was:\n%s", out)
	}
}

// The language reaches the two questions asked before the dashboard exists and
// nothing on it, so the reply has to say when the change will show — otherwise
// the user waits for a screen that is never going to turn.
func TestConsoleLangSavesTheChoiceAndSaysWhenItApplies(t *testing.T) {
	tempConfigDir(t)
	cfg := config.Default()

	out := consoleOutput(t, "lang\n2\n", consoleDeps{
		hub: testHub(t), sb: &statusBar{headless: true}, cfg: cfg,
	})

	if !strings.Contains(out, "from the next start") {
		t.Fatalf("lang did not say when the change applies; output was:\n%s", out)
	}
	if cfg.Language == "" {
		t.Error("the config does not record the language that was chosen")
	}
}

func TestConsoleLangStopsWhenTheSettingCannotBeSaved(t *testing.T) {
	unwritableConfigDir(t)

	out := consoleOutput(t, "lang\n2\n", consoleDeps{
		hub: testHub(t), sb: &statusBar{headless: true}, cfg: config.Default(),
	})

	if !strings.Contains(out, "Could not save the setting") {
		t.Fatalf("lang did not report the failed save; output was:\n%s", out)
	}
	if strings.Contains(out, "from the next start") {
		t.Error("lang promised a change it could not write down")
	}
}
