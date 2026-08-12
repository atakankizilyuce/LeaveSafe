package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/auth"
	ble "github.com/leavesafe/leavesafe/internal/bluetooth"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/eventlog"
	"github.com/leavesafe/leavesafe/internal/remote"
	"github.com/leavesafe/leavesafe/internal/safe"
	"github.com/leavesafe/leavesafe/internal/state"
	"github.com/leavesafe/leavesafe/internal/update"
)

// Starting up is the one thing that has to work. Everything else in this program
// is guarded by something; the start is what installs the guards, and until now
// nothing exercised it but running the program on a real machine.
//
// These tests do the start for real — config on disk, a listening socket, every
// supervised loop running — against a temporary config directory and an
// ephemeral port, and then shut it down again.

// startedApp starts the program against a config directory of its own and stops
// it when the test ends.
func startedApp(t *testing.T, prepare func(*config.Config)) *app {
	t.Helper()

	tempConfigDir(t)
	// A dashboard writes escape codes at absolute screen positions and takes the
	// logger with it, so the interactive tests point it at a file. This is the
	// headless shape, which is what an autostart entry gets.
	cfg := config.Default()
	cfg.Port = 0
	off := false
	cfg.UpdateCheck = &off
	cfg.RemoteAccess = &off
	if prepare != nil {
		prepare(cfg)
	}

	a, err := startApp(appOptions{cfg: cfg, headless: true, out: os.Stdout, in: strings.NewReader("")})
	if err != nil {
		t.Fatalf("startApp: %v", err)
	}
	t.Cleanup(func() {
		a.shutdown()
		a.close()
		// The panic handler and the log output are process-wide, and a start
		// installs both. Leaving them pointed at a finished test is how one test
		// writes into another one's assertions.
		safe.SetPanicHandler(nil)
	})
	return a
}

func TestStartupBringsUpAListeningServerAndAPairableKey(t *testing.T) {
	a := startedApp(t, nil)

	urls := a.srv.URLs()
	if len(urls) == 0 {
		t.Fatal("the server came up with no address to pair on")
	}
	// Every address has to be reachable, or the QR code drawn from it points
	// at nothing.
	for _, u := range urls {
		host := strings.TrimPrefix(u, "http://")
		conn, err := net.DialTimeout("tcp", host, 2*time.Second)
		if err != nil {
			t.Errorf("nothing is listening on %s: %v", u, err)
			continue
		}
		_ = conn.Close()
	}

	if a.sb == nil || len(a.sb.urlList()) == 0 {
		t.Error("the status bar offers no address")
	}
	if a.hub == nil || a.hub.IsArmed() {
		t.Error("a fresh start came up armed")
	}
}

// A headless start persists its pairing key, because it has no screen to show a
// QR code on: a freshly generated key would be a secret nobody could read, and a
// phone paired before the reboot would be locked out by a key it never saw.
func TestAHeadlessStartKeepsThePairingKeyAcrossRestarts(t *testing.T) {
	dir := tempConfigDir(t)
	cfg := config.Default()
	cfg.Port = 0
	off := false
	cfg.UpdateCheck = &off
	cfg.RemoteAccess = &off

	first, err := startApp(appOptions{cfg: cfg, headless: true, in: strings.NewReader("")})
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	firstKey := first.sb.key
	first.shutdown()
	first.close()

	keyFile := filepath.Join(config.ConfigDir(), auth.KeyFileName)
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("no pairing key was persisted under %s: %v", dir, err)
	}

	second, err := startApp(appOptions{cfg: cfg, headless: true, in: strings.NewReader("")})
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	t.Cleanup(func() {
		second.shutdown()
		second.close()
		safe.SetPanicHandler(nil)
	})

	if second.sb.key != firstKey {
		t.Errorf("the key changed across a restart: %q then %q — every paired phone is locked out",
			firstKey, second.sb.key)
	}
}

// The previous run's state is read before anything can overwrite it. If that run
// ended while armed, the machine has been unwatched ever since and the user has
// to be told — coming back up quietly disarmed presents that gap as a normal
// start.
func TestStartupReportsARunThatEndedWhileArmed(t *testing.T) {
	tempConfigDir(t)
	if err := os.MkdirAll(config.ConfigDir(), 0o700); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	store := state.NewStore(config.ConfigDir(), version)
	if err := store.Save(true); err != nil {
		t.Fatalf("seed the previous state: %v", err)
	}

	cfg := config.Default()
	cfg.Port = 0
	off := false
	cfg.UpdateCheck = &off
	cfg.RemoteAccess = &off

	out := captureLog(t, func() {
		a, err := startApp(appOptions{cfg: cfg, headless: true, in: strings.NewReader("")})
		if err != nil {
			t.Fatalf("startApp: %v", err)
		}
		a.shutdown()
		a.close()
		safe.SetPanicHandler(nil)
	})

	if !strings.Contains(out, "armed") {
		t.Errorf("the interrupted run was not reported; output was:\n%s", out)
	}
}

// Every recovered panic has to reach the user. safe logs the stack; the handler
// this start installs is what puts it in the event history and on screen.
func TestStartupSendsARecoveredPanicToTheEventLog(t *testing.T) {
	startedApp(t, nil)

	// safe.Do rather than safe.Go: it recovers on the calling goroutine, so the
	// handler has run by the time it returns. Waiting on the panicking goroutine
	// instead would race the recovery that follows it.
	if safe.Do("a-loop-with-a-bug", func() { panic("something went wrong") }) {
		t.Fatal("the panic did not happen")
	}

	events, err := eventlog.ReadLast(filepath.Join(config.ConfigDir(), eventLogFileName), 20)
	if err != nil {
		t.Fatalf("read the event log: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Type == eventlog.EventPanic && strings.Contains(ev.Message, "a-loop-with-a-bug") {
			found = true
		}
	}
	if !found {
		t.Errorf("a recovered panic never reached the event history: %+v", events)
	}
}

// Auto-arm watches the screen, so the screen sensor has to be running before the
// lock that triggers it. Starting it lazily would mean the first lock after a
// reboot is the one nothing is watching.
func TestAutoArmStartsTheScreenSensorAtStartup(t *testing.T) {
	a := startedApp(t, func(cfg *config.Config) { cfg.AutoArmOnLock = true })

	if !a.sensors.IsEnabled("screen") {
		t.Error("auto-arm is on but the screen sensor is not")
	}
}

// Serving and stopping is the ordinary lifecycle, and the important part is that
// a shutdown the program asked for is not reported as a server error: treating
// it as one made every clean Ctrl+C end on a red FATAL line.
func TestServeReturnsCleanlyWhenTheProgramShutsItselfDown(t *testing.T) {
	tempConfigDir(t)
	cfg := config.Default()
	cfg.Port = 0
	off := false
	cfg.UpdateCheck = &off
	cfg.RemoteAccess = &off

	a, err := startApp(appOptions{cfg: cfg, headless: true, in: strings.NewReader("")})
	if err != nil {
		t.Fatalf("startApp: %v", err)
	}
	t.Cleanup(func() { safe.SetPanicHandler(nil) })

	served := make(chan error, 1)
	go func() { served <- a.serve() }()

	// Give the server a moment to be serving rather than starting.
	time.Sleep(50 * time.Millisecond)
	a.shutdown()
	a.close()

	select {
	case err := <-served:
		if err != nil {
			t.Errorf("a shutdown this program asked for was reported as a server error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve never returned after the shutdown")
	}
}

// A port already in use is the common startup failure — a second copy, or
// something else on 8080. Refusing to start would be the wrong answer: the
// machine is unwatched either way, and the user is not there to read the reason.
// The server takes any free port instead, which is why the address is drawn as a
// QR code rather than assumed.
func TestAStartOnABusyPortMovesToAFreeOneRatherThanGivingUp(t *testing.T) {
	held, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}
	t.Cleanup(func() { _ = held.Close() })
	_, portText, err := net.SplitHostPort(held.Addr().String())
	if err != nil {
		t.Fatalf("split the held address: %v", err)
	}
	busy, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse the held port: %v", err)
	}

	a := startedApp(t, func(cfg *config.Config) { cfg.Port = busy })

	urls := a.sb.urlList()
	if len(urls) == 0 {
		t.Fatal("the start gave up rather than moving")
	}
	for _, u := range urls {
		if strings.HasSuffix(u, ":"+portText) {
			t.Errorf("the server claims to be on the busy port: %s", u)
		}
	}
}

// The interactive shape: a dashboard on screen and a console reading typed
// commands. The console's reader is exhausted here, which is what a terminal
// that closes does.
func TestAnInteractiveStartDrawsTheDashboardAndTakesTheLog(t *testing.T) {
	tempConfigDir(t)
	cfg := config.Default()
	cfg.Port = 0
	off := false
	cfg.UpdateCheck = &off
	cfg.RemoteAccess = &off

	screen, err := os.Create(filepath.Join(t.TempDir(), "screen"))
	if err != nil {
		t.Fatalf("create the screen file: %v", err)
	}
	t.Cleanup(func() { _ = screen.Close() })

	// A dashboard takes the logger over, so it is put back before anything else
	// reads it.
	previous := log.StandardLogger().Out
	t.Cleanup(func() { log.SetOutput(previous) })

	a, err := startApp(appOptions{
		cfg: cfg, headless: false, out: screen, in: strings.NewReader(""),
	})
	if err != nil {
		t.Fatalf("startApp: %v", err)
	}
	t.Cleanup(func() {
		a.shutdown()
		a.close()
		safe.SetPanicHandler(nil)
	})

	if err := screen.Sync(); err != nil {
		t.Fatalf("flush the screen file: %v", err)
	}
	drawn, err := os.ReadFile(screen.Name())
	if err != nil {
		t.Fatalf("read the screen back: %v", err)
	}
	if !strings.Contains(string(drawn), "Scan to connect:") {
		t.Errorf("no dashboard was drawn; screen was:\n%s", string(drawn))
	}
	if a.sb.headless {
		t.Error("an interactive start produced a headless status bar")
	}
}

// -plain is between the other two: nobody is looking at a headless start, and
// somebody is looking at this one. So there is no dashboard drawn over the
// window the user was working in, but there is still a code to scan, and the
// console still reads what they type.
func TestAPlainStartPrintsTheCodeWithoutTakingTheTerminal(t *testing.T) {
	tempConfigDir(t)
	cfg := config.Default()
	cfg.Port = 0
	off := false
	cfg.UpdateCheck = &off
	cfg.RemoteAccess = &off

	screen, err := os.Create(filepath.Join(t.TempDir(), "screen"))
	if err != nil {
		t.Fatalf("create the screen file: %v", err)
	}
	t.Cleanup(func() { _ = screen.Close() })

	a, err := startApp(appOptions{
		cfg: cfg, plain: true, out: screen, in: strings.NewReader(""),
	})
	if err != nil {
		t.Fatalf("startApp: %v", err)
	}
	t.Cleanup(func() {
		a.shutdown()
		a.close()
		safe.SetPanicHandler(nil)
	})

	if err := screen.Sync(); err != nil {
		t.Fatalf("flush the screen file: %v", err)
	}
	drawn, err := os.ReadFile(screen.Name())
	if err != nil {
		t.Fatalf("read the screen back: %v", err)
	}

	printed := string(drawn)
	if !strings.Contains(printed, "Scan to connect:") {
		t.Errorf("a plain start offered nothing to scan; output was:\n%s", printed)
	}
	if strings.Contains(printed, "\033[2J") || strings.Contains(printed, altScreenOn) {
		t.Error("a plain start took the terminal over anyway")
	}
	if !a.sb.headless {
		t.Error("a plain start drew a dashboard it has no room for")
	}
}

// A cleartext PIN left by an older version is hashed and the config rewritten,
// so the digits themselves no longer live on disk. They guard the alarm, and a
// config file is not a secret store.
func TestStartupHashesACleartextPinAndForgetsIt(t *testing.T) {
	tempConfigDir(t)
	cfg := config.Default()
	cfg.PinProtection.Enabled = true
	cfg.PinProtection.Pin = "2468"

	migratePlaintextPin(cfg)

	if cfg.PinProtection.Pin != "" {
		t.Error("the cleartext PIN is still in the config")
	}
	if cfg.PinProtection.PinHash == "" {
		t.Fatal("no hash was stored, so the PIN was simply lost")
	}
	if cfg.PinProtection.PinHash == "2468" {
		t.Error("the hash is the PIN itself")
	}
	// And it has to survive the rewrite, or the next start asks for a PIN
	// nobody set.
	saved, err := config.Load()
	if err != nil {
		t.Fatalf("reload the config: %v", err)
	}
	if saved.PinProtection.PinHash != cfg.PinProtection.PinHash {
		t.Error("the migrated hash was not written to disk")
	}
	if saved.PinProtection.Pin != "" {
		t.Error("the cleartext PIN is still on disk")
	}
}

func TestAConfigWithNoPinIsLeftAlone(t *testing.T) {
	cfg := config.Default()
	cfg.PinProtection.PinHash = "already-hashed"

	migratePlaintextPin(cfg)

	if cfg.PinProtection.PinHash != "already-hashed" {
		t.Error("a config with nothing to migrate was rewritten anyway")
	}
}

// A headless start with no stored preference records one rather than asking, and
// says which mode it settled on — there is no terminal to say it to a person,
// but the log is what the user reads afterwards.
func TestAHeadlessStartRecordsAConnectionModeRatherThanAsking(t *testing.T) {
	tempConfigDir(t)
	cfg := config.Default()
	cfg.RemoteAccess = nil

	out := captureLog(t, func() { chooseConnectionMode(cfg, true) })

	if cfg.RemoteAccess == nil || *cfg.RemoteAccess {
		t.Error("a headless start did not settle on the local network")
	}
	if !strings.Contains(out, "no terminal to ask") {
		t.Errorf("the log does not say why it was not asked; output was:\n%s", out)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatalf("reload the config: %v", err)
	}
	if saved.RemoteAccess == nil {
		t.Error("the choice was not written down, so every start would make it again")
	}
}

func TestAHeadlessStartKeepsAStoredConnectionMode(t *testing.T) {
	tempConfigDir(t)
	cfg := config.Default()
	on := true
	cfg.RemoteAccess = &on

	chooseConnectionMode(cfg, true)

	if cfg.RemoteAccess == nil || !*cfg.RemoteAccess {
		t.Error("a stored preference for remote access was overwritten")
	}
}

// PORT overrides the config, but only when it names a port that could work.
// Falling back silently used to mean the server came up somewhere the user did
// not ask for, behind a QR code they would scan and wonder about.
func TestListenPortPrefersTheEnvironmentWhenItMakesSense(t *testing.T) {
	cases := map[string]struct {
		env  string
		want int
	}{
		"a port":             {"9000", 9000},
		"nothing set":        {"", 8080},
		"not a number":       {"eight thousand", 8080},
		"below the range":    {"-1", 8080},
		"above the range":    {"70000", 8080},
		"zero, for any port": {"0", 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("PORT", tc.env)
			} else {
				t.Setenv("PORT", "")
			}
			if got := listenPort(8080); got != tc.want {
				t.Errorf("listenPort = %d, want %d", got, tc.want)
			}
		})
	}
}

// A headless start that cannot write its key file has nowhere to keep the
// pairing, so it stops rather than coming up with a key that will be different
// after the next reboot and locking out every paired phone.
func TestAHeadlessStartStopsWhenItCannotKeepThePairingKey(t *testing.T) {
	unwritableConfigDir(t)

	_, _, err := resolvePairingKey(config.Default(), true)

	if err == nil {
		t.Fatal("a start with nowhere to store the key reported success")
	}
	if !strings.Contains(err.Error(), "stored pairing key") {
		t.Errorf("the failure does not say what went wrong: %v", err)
	}
}

// A key file edited by hand, or truncated by a crash. A key that fails its own
// check digit could never have been produced by this program, so it is replaced
// rather than trusted: coming up with it would mean a QR code that fails every
// pairing, on a machine with no screen to show a working one.
func TestADamagedStoredKeyIsReplacedRatherThanTrusted(t *testing.T) {
	tempConfigDir(t)
	if err := os.MkdirAll(config.ConfigDir(), 0o700); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	keyFile := filepath.Join(config.ConfigDir(), auth.KeyFileName)
	if err := os.WriteFile(keyFile, []byte("not-a-key\n"), 0o600); err != nil {
		t.Fatalf("write the damaged key file: %v", err)
	}

	mgr, keyPath, err := resolvePairingKey(config.Default(), true)
	if err != nil {
		t.Fatalf("resolvePairingKey: %v", err)
	}

	if keyPath != keyFile {
		t.Errorf("keyPath = %q, want %q", keyPath, keyFile)
	}
	if mgr.RawPairingKey() == "not-a-key" || mgr.RawPairingKey() == "" {
		t.Errorf("the damaged key was kept: %q", mgr.RawPairingKey())
	}
	stored, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("read the key file back: %v", err)
	}
	if strings.TrimSpace(string(stored)) != mgr.RawPairingKey() {
		t.Error("the replacement was not written down, so the next start would generate another")
	}
}

// The whole start stops on it, not just the key step: a half-started program
// that is listening but cannot be paired with is worse than one that says why.
func TestAStartStopsWhenThePairingKeyCannotBeResolved(t *testing.T) {
	unwritableConfigDir(t)
	cfg := config.Default()
	cfg.Port = 0

	a, err := startApp(appOptions{cfg: cfg, headless: true, in: strings.NewReader("")})

	if err == nil {
		a.shutdown()
		a.close()
		safe.SetPanicHandler(nil)
		t.Fatal("a start with nowhere to store the key reported success")
	}
	if !strings.Contains(err.Error(), "pairing key") {
		t.Errorf("the failure does not say what went wrong: %v", err)
	}
}

// A hand-edited config is the normal way to change most of these settings, and a
// typo should not produce a monitor that never heartbeats. What was corrected
// has to be said, or the user is left believing the value they typed is in
// force.
func TestLoadConfigSaysWhatItHadToCorrect(t *testing.T) {
	dir := tempConfigDir(t)
	if err := os.MkdirAll(config.ConfigDir(), 0o700); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	path := filepath.Join(config.ConfigDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"heartbeat_seconds": 999999}`), 0o600); err != nil {
		t.Fatalf("write the config in %s: %v", dir, err)
	}

	var cfg *config.Config
	out := captureLog(t, func() { cfg = loadConfig() })

	if !strings.Contains(out, "heartbeat_seconds") {
		t.Errorf("the correction was not reported; output was:\n%s", out)
	}
	if cfg.HeartbeatSeconds == 999999 {
		t.Error("the out-of-range value was left in force")
	}
}

// A headless start that cannot write its choice down still has to come up. The
// choice is remade next time, which costs nothing; refusing to start would cost
// the monitoring.
func TestAHeadlessStartCarriesOnWhenItCannotRecordTheMode(t *testing.T) {
	unwritableConfigDir(t)
	cfg := config.Default()
	cfg.RemoteAccess = nil

	out := captureLog(t, func() { chooseConnectionMode(cfg, true) })

	if cfg.RemoteAccess == nil {
		t.Error("no mode was settled on")
	}
	if !strings.Contains(out, "Failed to save config") {
		t.Errorf("the failed write was silent; output was:\n%s", out)
	}
}

// An interactive start generates its key instead, because there is a screen to
// show it on.
func TestAnInteractiveStartGeneratesAKeyAndStoresItNowhere(t *testing.T) {
	tempConfigDir(t)

	mgr, keyPath, err := resolvePairingKey(config.Default(), false)
	if err != nil {
		t.Fatalf("resolvePairingKey: %v", err)
	}

	if keyPath != "" {
		t.Errorf("an interactive start named a key file at %q", keyPath)
	}
	if mgr.RawPairingKey() == "" {
		t.Error("no pairing key was generated")
	}
}

// The migration is reported when it cannot be written down, because the next
// start will find the cleartext PIN still sitting there and try again.
func TestAMigratedPinThatCannotBeSavedIsReported(t *testing.T) {
	unwritableConfigDir(t)
	cfg := config.Default()
	cfg.PinProtection.Enabled = true
	cfg.PinProtection.Pin = "2468"

	out := captureLog(t, func() { migratePlaintextPin(cfg) })

	if !strings.Contains(out, "Failed to save config after PIN migration") {
		t.Errorf("a failed migration was silent; output was:\n%s", out)
	}
}

// Nothing about a start may be fatal because a directory could not be made. The
// event history and the log file are worth having and worth complaining about,
// but the machine still has to be watched without them.
func TestAStartWithNowhereToWriteStillWatchesTheMachine(t *testing.T) {
	unwritableConfigDir(t)
	cfg := config.Default()
	cfg.Port = 0
	off := false
	cfg.UpdateCheck = &off
	cfg.RemoteAccess = &off

	var a *app
	out := captureLog(t, func() {
		var err error
		a, err = startApp(appOptions{cfg: cfg, headless: false, out: os.Stdout, in: strings.NewReader("")})
		if err != nil {
			t.Fatalf("startApp: %v", err)
		}
	})
	t.Cleanup(func() {
		a.shutdown()
		a.close()
		safe.SetPanicHandler(nil)
	})

	if len(a.srv.URLs()) == 0 {
		t.Error("the server did not come up")
	}
	for _, want := range []string{"Failed to create config dir", "Failed to open event log"} {
		if !strings.Contains(out, want) {
			t.Errorf("the start did not report %q; output was:\n%s", want, out)
		}
	}
}

// A log hook that was already installed has to survive a start and a shutdown.
// Taking the file hook back out must not take the rest of them with it.
func TestShuttingDownLeavesHooksThatWereAlreadyThere(t *testing.T) {
	tempConfigDir(t)
	existing := &countingHook{}
	log.AddHook(existing)
	t.Cleanup(func() { log.StandardLogger().ReplaceHooks(log.LevelHooks{}) })

	a := startedApp(t, nil)
	a.close()

	before := existing.count()
	log.Info("a line written after the start was closed")
	if existing.count() == before {
		t.Error("closing the start took a hook it did not install")
	}
}

// countingHook stands in for a log hook that was installed before the program
// started, and counts what it was asked to write.
//
// The count is behind a lock for the same reason syncBuffer is: logrus fires a
// hook on whichever goroutine wrote the line, and a started program still has
// supervised loops logging while the test reads the count. Without the lock that
// is a data race between them, and -race is right to say so — it failed this
// test on the macOS runner, where the timing happened to line up.
type countingHook struct {
	mu    sync.Mutex
	fired int
}

func (h *countingHook) Levels() []log.Level { return log.AllLevels }

func (h *countingHook) Fire(*log.Entry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fired++
	return nil
}

func (h *countingHook) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fired
}

// The alarm callbacks are what turn an alert into a noise on this machine, and
// a dismissal into silence. Wiring them the wrong way round would leave a siren
// nothing could stop.
func TestAnAlertStartsTheLocalAlarmAndDismissingItStops(t *testing.T) {
	a := startedApp(t, func(cfg *config.Config) {
		// An escalation whose only step notifies the phone, and a delay it never
		// reaches, so this sounds nothing on the machine running the tests.
		cfg.Alarm.EscalationEnabled = true
		cfg.Alarm.Levels = []config.AlarmLevel{{DelaySeconds: 3600, Action: "notify_phone_only"}}
		// Arming starts every enabled sensor, and a test has no business
		// polling this machine's hardware. They stay registered, which is all
		// a manual trigger needs.
		cfg.EnabledSensors = map[string]bool{}
		for _, name := range []string{"power", "lid", "usb", "screen", "network", "input"} {
			cfg.EnabledSensors[name] = false
		}
	})

	sensors := a.sensors.Sensors()
	if len(sensors) == 0 {
		t.Fatal("no sensors were registered, so nothing can raise an alarm")
	}

	a.hub.Arm()
	if !a.hub.TriggerSensorTest(sensors[0].Name()) {
		t.Fatalf("the %q sensor could not be triggered", sensors[0].Name())
	}

	deadline := time.Now().Add(5 * time.Second)
	for !a.alarm.IsPlaying() {
		if time.Now().After(deadline) {
			t.Fatal("an alert never reached the local alarm")
		}
		time.Sleep(time.Millisecond)
	}

	a.hub.DismissAlarm()

	for a.alarm.IsPlaying() {
		if time.Now().After(deadline) {
			t.Fatal("dismissing the alarm did not stop it")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTheDashboardIsToldWhenTheLastPhoneGoes(t *testing.T) {
	a := startedApp(t, nil)

	out := captureLog(t, func() { a.reportNoPhoneConnected() })

	if !strings.Contains(out, "No phone is connected") {
		t.Errorf("nothing said the alerts may not arrive; output was:\n%s", out)
	}
	if !strings.Contains(out, "Monitoring continues") {
		t.Errorf("the message reads as though monitoring had stopped; output was:\n%s", out)
	}
}

func TestTheDashboardIsRepaintedWhenAPhoneComesOrGoes(t *testing.T) {
	a := startedApp(t, nil)

	// The headless status bar draws nothing; what matters is that the callback
	// is wired to something that does not panic on a count it did not expect.
	a.clientsChanged(0, false)
	a.clientsChanged(2, true)
}

// Turning remote access off pushes the result the same way turning it on does,
// so the dashboard and every paired phone agree on what is running.
func TestTurningRemoteAccessOffPublishesTheResult(t *testing.T) {
	a := startedApp(t, nil)

	a.setRemoteAccess(t.Context(), false)

	if a.sb.remoteStatus != "" {
		t.Errorf("remoteStatus = %q, want it empty with remote access off", a.sb.remoteStatus)
	}
}

// A reachability answer arriving on its own has to land the same way a change
// made from the console does — it is the only thing that turns "checking" into
// an address a phone on another network can use.
func TestAReachabilityAnswerReachesTheDashboard(t *testing.T) {
	a := startedApp(t, nil)

	a.publishRemoteState(remote.State{
		Enabled:   true,
		UPnP:      remote.UPnPOK,
		CertFP:    "AA:BB:CC",
		PublicURL: "https://198.51.100.4:9443",
	})

	if !strings.Contains(a.sb.remoteStatus, "ACTIVE") {
		t.Errorf("remoteStatus = %q, want it to report remote access as active", a.sb.remoteStatus)
	}
	if got := a.sb.certFingerprint(); got != "AA:BB:CC" {
		t.Errorf("certFingerprint = %q, want the one the state carried", got)
	}
	urls := a.sb.urlList()
	if len(urls) == 0 || urls[0] != "https://198.51.100.4:9443" {
		t.Errorf("the public address is not the one the QR code shows: %v", urls)
	}
}

// A newer release marks the footer and nothing else. It is not an emergency, and
// the panel belongs to the sensors.
func TestAnAvailableUpdateIsReportedQuietly(t *testing.T) {
	a := startedApp(t, nil)

	out := captureLog(t, func() {
		a.reportUpdate(update.Result{
			Available: true,
			Latest:    "9.9.9",
			URL:       "https://example.invalid/releases",
		}, update.MethodUnknown)
	})

	if !strings.Contains(out, "9.9.9") {
		t.Errorf("the new version was not named; output was:\n%s", out)
	}
}

// Three things can become of the Bluetooth server, and they ask different things
// of the user: nothing, pair over Wi-Fi instead, or a bug worth reporting.
func TestBluetoothSaysWhichOfItsThreeOutcomesHappened(t *testing.T) {
	a := startedApp(t, nil)

	if out := captureLog(t, func() { a.reportBluetooth(nil) }); strings.Contains(out, "BLE") {
		t.Errorf("a Bluetooth server that started fine said something anyway:\n%s", out)
	}

	out := captureLog(t, func() { a.reportBluetooth(ble.ErrNoCentralIdentity) })
	if !strings.Contains(out, "Bluetooth pairing is off on this platform") {
		t.Errorf("a platform without Bluetooth was not explained; output was:\n%s", out)
	}
	// Saying what still works is the point: the user asked for Bluetooth and is
	// not getting it, and Wi-Fi is right there.
	if !strings.Contains(out, "Pair over Wi-Fi instead") {
		t.Errorf("nothing offered the alternative; output was:\n%s", out)
	}

	out = captureLog(t, func() { a.reportBluetooth(errors.New("the adapter caught fire")) })
	if !strings.Contains(out, "the adapter caught fire") {
		t.Errorf("an unexpected Bluetooth failure was swallowed; output was:\n%s", out)
	}
}

// Bluetooth is off unless the config asks for it, and asking for it must not be
// what decides whether Wi-Fi works.
func TestBluetoothIsOnlyStartedWhenTheConfigAsksForIt(t *testing.T) {
	a := startedApp(t, func(cfg *config.Config) { cfg.ConnectionMode = "wifi" })

	if a.srv == nil || len(a.srv.URLs()) == 0 {
		t.Error("a Wi-Fi start did not come up on the network")
	}
}

func TestBluetoothStartsAlongsideWiFiWhenAskedForBoth(t *testing.T) {
	a := startedApp(t, func(cfg *config.Config) { cfg.ConnectionMode = "both" })

	// Whether this platform can actually offer BLE is not the point — plenty
	// cannot, and the program says so and carries on. What must hold is that
	// asking for it leaves the network path working.
	if a.srv == nil || len(a.srv.URLs()) == 0 {
		t.Error("asking for Bluetooth took the network path down with it")
	}
}

// Reading the keyboard a keystroke at a time means reading the Ctrl+C that used
// to become SIGINT before it becomes one, so the handler in main never fires
// and nothing else would stop the program. This is that handler's work done by
// hand — and it has to be the same orderly shutdown, not a quicker way out.
func TestQuitShutsTheProgramDownAndLeavesWithNothingToReport(t *testing.T) {
	a := startedApp(t, nil)

	left := -1
	previous := exitProcess
	exitProcess = func(code int) { left = code }
	t.Cleanup(func() { exitProcess = previous })

	a.quit()

	if left != 0 {
		t.Errorf("the program left with status %d, want 0 — nothing went wrong", left)
	}
	// The shutdown really happened rather than the program simply stopping:
	// an armed machine that exits without recording it comes back believing it
	// was never watching.
	if err := a.serve(); err != nil {
		t.Errorf("the server was still running after quit: %v", err)
	}
}
