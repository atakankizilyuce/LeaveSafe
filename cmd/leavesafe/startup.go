package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/alarm"
	"github.com/leavesafe/leavesafe/internal/auth"
	ble "github.com/leavesafe/leavesafe/internal/bluetooth"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/endpoint"
	"github.com/leavesafe/leavesafe/internal/eventlog"
	"github.com/leavesafe/leavesafe/internal/logsink"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/push"
	"github.com/leavesafe/leavesafe/internal/safe"
	"github.com/leavesafe/leavesafe/internal/server"
	"github.com/leavesafe/leavesafe/internal/state"
	"github.com/leavesafe/leavesafe/internal/update"
	"github.com/leavesafe/leavesafe/internal/ws"
)

// appOptions is what the command line decided, plus the two streams the program
// talks to a person through.
//
// The streams are parameters rather than os.Stdout and os.Stdin directly, for
// the same reason runConsole already takes a reader: it is what lets a start be
// driven from a test. The running program passes the real ones.
type appOptions struct {
	cfg      *config.Config
	headless bool
	// plain keeps the terminal usable: log lines and typed commands, with no
	// full-screen dashboard drawn over the window the user was working in. It
	// differs from headless in that there is still somebody at the keyboard, so
	// the console loop runs and the pairing code is printed once.
	plain   bool
	devMode bool
	// managed says this copy was installed and is started by the LeaveSafe
	// desktop application, which ships the binary and decides which version it
	// is. It changes one thing: this copy does not ask GitHub about releases.
	// See superviseUpdateCheck.
	managed bool
	// out is where the dashboard is drawn. Unused on a headless start, which
	// has no terminal to draw on.
	out *os.File
	// in is where typed commands are read from. Unused on a headless start,
	// which has no terminal to read from either.
	in io.Reader
}

// app is a LeaveSafe that has been wired together and started: every loop is
// running and the server is listening, but it is not yet serving.
//
// Splitting this out of main is what makes the startup order testable, and the
// order is where the risk in this file lives. The listener opens before the
// dashboard is drawn from its addresses; the previous run's state is read before
// anything can overwrite it; the pairing key is resolved before the QR codes
// that encode it. Until now none of that was exercised by anything except a real
// start on a real machine.
type app struct {
	hub     *ws.Hub
	srv     *server.Server
	sb      *statusBar
	sensors *monitor.Manager
	alarm   *alarm.Alarm

	cancel context.CancelFunc
	// closers are the files this start opened, closed in the order that makes
	// the last thing written to the log the shutdown itself.
	closers []func() error
}

// startApp wires everything together and starts every loop.
//
// It returns rather than exits on failure. The three ways a start can fail are
// all worth the caller deciding about: no stored key it can read, no auth
// manager, and no port to listen on.
func startApp(opts appOptions) (*app, error) {
	cfg := opts.cfg
	migratePlaintextPin(cfg)

	authMgr, keyPath, err := resolvePairingKey(cfg, opts.headless)
	if err != nil {
		return nil, err
	}

	sensorMgr := monitor.NewManager()
	registerSensors(sensorMgr, cfg)

	hub := ws.NewHub(authMgr, sensorMgr, version)
	hub.SetConfig(cfg)
	hub.SetTimings(
		time.Duration(cfg.HeartbeatSeconds)*time.Second,
		time.Duration(cfg.DisconnectGraceSeconds)*time.Second,
	)

	// Somewhere to reach a phone that is not connected. A failure here costs
	// the push alerts and nothing else: the siren, the local network and every
	// connected phone are unaffected, and the alternative — refusing to start
	// the alarm system because a key file could not be written — helps nobody.
	if notifier, err := push.Open(config.ConfigDir()); err != nil {
		log.Warnf("Push alerts are unavailable: %v", err)
	} else {
		hub.SetPushNotifier(notifier)
	}

	a := &app{hub: hub, sensors: sensorMgr}
	a.openEventLog()

	// Read what the previous run left behind before anything can overwrite it.
	// If that run ended while armed, the machine has been unwatched ever since
	// and the user is about to be told so.
	stateStore := state.NewStore(config.ConfigDir(), version)
	prevState, prevStateErr := stateStore.Load()
	hub.SetStateStore(stateStore)

	a.openLogFile()

	// The listener opens once here and is never closed again: everything that
	// changes while the program runs changes around it rather than rebinding it,
	// so a phone already connected stays connected.
	a.srv = server.New(server.Config{Hub: hub, Port: listenPort(cfg.Port), DevMode: opts.devMode})
	if err := a.srv.Listen(); err != nil {
		return nil, fmt.Errorf("failed to bind port: %w", err)
	}
	// After Listen, because the port is not known until then, and as a function
	// rather than a list, because a laptop moves between networks while it runs.
	// This is the only thing that knows which addresses a phone could dial —
	// see ws.ServerMessage.Addresses for why the client is not asked to.
	hub.SetAddresses(a.srv.URLs)
	a.publishEndpoint(version)

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	a.sb = a.drawInterface(opts, authMgr, keyPath)
	a.installPanicHandler()

	a.alarm = alarm.New(cfg.Alarm)
	a.applyGuardSettings(cfg)

	reportInterruptedMonitoring(a.sb, hub, stateStore, prevState, prevStateErr, cfg.RestoreArmedState)

	// How this copy was installed decides what the user is told to run. Resolved
	// once: the binary does not move while it is running.
	installMethod := update.DetectSelf()
	updateLedger := update.NewLedger(config.ConfigDir())

	a.wireCallbacks()
	hub.SetLocationTracker(ctx, buildLocationTracker(cfg))

	// Which sensors can work here is reported once, in the background, because
	// finding out can be slow and nothing about serving should wait on it.
	safe.Go("sensor-availability", func() { logSensorAvailability(sensorMgr) })

	a.superviseLoops(ctx, opts, consoleDeps{
		hub: hub, sb: a.sb, localAlarm: a.alarm, authMgr: authMgr,
		installMethod: installMethod, updateLedger: updateLedger,
		srv: a.srv, cfg: cfg, quit: a.quit,
	})
	a.superviseUpdateCheck(ctx, cfg, installMethod, updateLedger, opts.managed)
	a.superviseBluetooth(ctx, cfg)

	return a, nil
}

// serve blocks until the server stops.
//
// A shutdown this program asked for is not a server error. shutdown calls the
// server's own Shutdown, which makes Serve return ErrServerClosed here — and
// treating that as fatal meant every clean Ctrl+C ended on a red FATAL line and
// an exit status of 1, racing the orderly exit the signal handler was already
// running. Anything else really is the listener dying under a machine the user
// believes is being watched.
func (a *app) serve() error {
	if err := a.srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// shutdown stops everything this start began.
func (a *app) shutdown() {
	// Before anything else and before a single line is printed: the dashboard
	// pinned a scrolling region across an alternate screen, and every word
	// written from here belongs to the user's own terminal.
	terminalScreen.restore()
	// And the dashboard is told so, in the same breath. Everything below still
	// logs — the sensors stopping, the server closing — and until this was here
	// those lines were drawn the way the dashboard draws, onto a screen it no
	// longer had: the status grid repainted across the user's shell with their
	// cursor left on whichever row the input row had been.
	a.sb.handBack()
	fmt.Printf("  %sShutting down…%s\n", cDim, cReset)

	a.cancel()
	a.alarm.Stop()
	a.sensors.StopAll()
	if el := a.hub.EventLogger(); el != nil {
		_ = el.Close()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := a.srv.Shutdown(shutdownCtx); err != nil {
		log.Warnf("Server shutdown: %v", err)
	}
}

// quit ends the program, for the Ctrl+C the terminal no longer delivers.
//
// Reading the keyboard a keystroke at a time means reading the keystroke that
// used to become SIGINT before it became one, so the signal handler in main
// never fires and nothing else would stop the program. This is that handler's
// work done by hand: the same orderly shutdown, and then out.
func (a *app) quit() {
	a.shutdown()
	exitProcess(0)
}

// exitProcess is os.Exit, as a variable so a test can watch a program end
// without ending the test binary with it. Nothing in the running program
// reassigns it.
var exitProcess = os.Exit

// close releases what the start opened, whether or not it ever served. It is
// separate from shutdown because a start that failed has files to close and no
// server to stop.
func (a *app) close() {
	// A start that failed halfway may already have drawn the dashboard, and the
	// caller's next move is to exit. Idempotent, so the ordinary path — where
	// shutdown has already done this — costs nothing.
	terminalScreen.restore()

	if a.cancel != nil {
		a.cancel()
	}
	for _, closer := range a.closers {
		_ = closer()
	}
}

// publishEndpoint writes down which port the listener took.
//
// The port is not knowable from outside this process — it defaults to zero and
// the operating system picks a free one at every start — so a client running as
// the same user has the pairing key sitting beside it in the config directory
// and no idea where to spend it. Writing the address down is what lets it open
// the machine it is already on without asking anybody to read an address off a
// screen.
//
// A failure here costs that convenience and nothing else: the siren, the local
// network and every connected phone are unaffected. The same trade as the push
// key above — refusing to start an alarm system because a file could not be
// written helps nobody.
func (a *app) publishEndpoint(version string) {
	dir := config.ConfigDir()
	err := endpoint.Publish(dir, endpoint.Endpoint{
		Port:      a.srv.Port(),
		PID:       os.Getpid(),
		Version:   version,
		StartedAt: time.Now(),
	})
	if err != nil {
		log.Warnf("Could not write down where this is listening: %v", err)
		return
	}

	// Withdrawn on the way out, by the path that runs whether or not the start
	// ever got as far as serving. What a crash leaves behind is a port that
	// refuses connections rather than a file that keeps claiming one.
	a.closers = append(a.closers, func() error { return endpoint.Withdraw(dir) })
}

// migratePlaintextPin hashes a cleartext PIN left by an older version and
// rewrites the config, so the digits themselves no longer live on disk.
func migratePlaintextPin(cfg *config.Config) {
	if cfg.PinProtection.Pin == "" {
		return
	}

	hash, err := auth.HashPin(cfg.PinProtection.Pin)
	if err != nil {
		log.Warnf("Failed to hash stored PIN: %v", err)
		return
	}

	cfg.PinProtection.PinHash = hash
	cfg.PinProtection.Pin = ""
	if err := config.Save(cfg); err != nil {
		log.Warnf("Failed to save config after PIN migration: %v", err)
	}
}

// resolvePairingKey builds the auth manager, and on a headless start hands it
// the key persisted on disk.
//
// A headless start has no screen to show a QR code on, so a freshly generated
// key would be a secret nobody could read — and a phone paired before the reboot
// would be locked out by a key it never saw. The persisted key is what makes the
// pairing survive the restart. It returns the path so the console can rewrite it
// when the key is rotated.
func resolvePairingKey(cfg *config.Config, headless bool) (*auth.Manager, string, error) {
	opts := auth.Options{
		MaxSessions:   cfg.MaxSessions,
		MaxAttempts:   cfg.MaxAuthAttempts,
		LockoutPeriod: time.Duration(cfg.LockoutSeconds) * time.Second,
		SessionTTL:    time.Duration(cfg.SessionTTLMinutes) * time.Minute,
		SessionIdle:   time.Duration(cfg.SessionIdleMinutes) * time.Minute,
	}

	keyPath := ""
	if headless {
		keyPath = filepath.Join(config.ConfigDir(), auth.KeyFileName)
		persisted, err := auth.LoadOrCreateKeyFile(keyPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to prepare the stored pairing key: %w", err)
		}
		opts.PairingKey = persisted
	}

	mgr, err := auth.NewManagerWithOptions(opts)
	if err != nil {
		return nil, "", fmt.Errorf("failed to initialize auth: %w", err)
	}
	return mgr, keyPath, nil
}

// listenPort is the port to serve on: the configured one, unless the
// environment names another.
//
// A PORT that cannot be used is said out loud rather than silently ignored.
// Falling back quietly used to mean the server came up on a port the user did
// not ask for, with a QR code they would scan and wonder about.
func listenPort(configured int) int {
	v := os.Getenv("PORT")
	if v == "" {
		return configured
	}

	parsed, err := strconv.Atoi(v)
	switch {
	case err != nil:
		log.Warnf("Ignoring PORT=%q: not a number. Using port %d from the config.", v, configured)
	case parsed < 0 || parsed > 65535:
		log.Warnf("Ignoring PORT=%d: outside the valid range 0-65535. Using port %d from the config.", parsed, configured)
	default:
		return parsed
	}
	return configured
}

func (a *app) openEventLog() {
	if err := os.MkdirAll(config.ConfigDir(), 0o700); err != nil {
		log.Warnf("Failed to create config dir: %v", err)
	}

	evLog, err := eventlog.New(filepath.Join(config.ConfigDir(), eventLogFileName))
	if err != nil {
		log.Warnf("Failed to open event log: %v", err)
		return
	}
	a.hub.SetEventLogger(evLog)
	a.closers = append(a.closers, evLog.Close)
}

// openLogFile keeps a copy of the log on disk. The terminal log scrolls away and
// the window eventually closes; the file is what is left to answer "why did
// nothing happen last Tuesday".
func (a *app) openLogFile() {
	hook, err := newFileHook(config.ConfigDir())
	if err != nil {
		log.Warnf("Failed to open the log file, logging to the terminal only: %v", err)
		return
	}

	// The logger is process-wide, so the hook has to come back out before the
	// file it writes to is closed. Left in, every log line after the close tries
	// to fire a hook whose file is gone and prints a complaint of its own.
	restore := hooksAsTheyAre()
	log.AddHook(hook)
	a.closers = append(a.closers, func() error {
		log.StandardLogger().ReplaceHooks(restore)
		return hook.Close()
	})
}

// hooksAsTheyAre is a copy of the logger's current hooks, taken so that adding
// one can be undone. ReplaceHooks is used to read them because it is the only
// accessor that takes the logger's lock.
func hooksAsTheyAre() log.LevelHooks {
	current := log.StandardLogger().ReplaceHooks(log.LevelHooks{})
	copied := log.LevelHooks{}
	for level, hooks := range current {
		copied[level] = append([]log.Hook(nil), hooks...)
	}
	log.StandardLogger().ReplaceHooks(current)
	return copied
}

// drawInterface puts the program on screen, or into the log when there is no
// screen to put it on.
func (a *app) drawInterface(opts appOptions, authMgr *auth.Manager, keyPath string) *statusBar {
	if opts.headless || opts.plain {
		sb := newHeadlessStatusBar(a.hub, a.sensors, authMgr.PairingKey(), authMgr.RawPairingKey(),
			a.srv.URLs(), keyPath)
		mode := "headless"
		if opts.plain {
			mode = "without the dashboard"
		}
		logHeadlessStartup(sb, mode)
		if opts.plain {
			// Somebody is watching this one, and without a dashboard there is
			// nowhere else a code to scan could appear. Printed rather than
			// positioned, so it scrolls away with everything else.
			sb.plainOut = opts.out
			printPairingCode(opts.out, sb, authMgr.RawPairingKey())
		}
		return sb
	}

	sb := buildDashboard(opts.out, opts.in, a.srv, authMgr, a.hub, a.sensors)
	// The dashboard owns the terminal, so log lines have to be routed through it
	// to land inside its scrolling region rather than on top of the QR code.
	// Headless has no such constraint.
	//
	// Through the sink, like the output it replaces. A terminal is not a safe
	// place to write either: selecting text in a Windows console pauses every
	// write to it until the selection is cleared, and a paused console must not
	// be able to stop this machine watching — see internal/logsink.
	log.SetOutput(logsink.New(&logWriter{sb: sb}, 0))
	return sb
}

// installPanicHandler puts every recovered panic somewhere the user will see it.
//
// A recovered panic is still a bug, and one that happened while the machine was
// supposed to be watching itself. safe already logged the stack; this puts it in
// the event history next to arm and disarm, and on the dashboard.
func (a *app) installPanicHandler() {
	safe.SetPanicHandler(func(name string, value any, _ []byte) {
		if el := a.hub.EventLogger(); el != nil {
			el.Log(eventlog.Event{
				Type:    eventlog.EventPanic,
				Sensor:  name,
				Message: fmt.Sprintf("Recovered from a panic in %s: %v", name, value),
			})
		}
		a.sb.writeLine("  %s[BUG]%s Recovered from a panic in %s: %v — please report this at %s",
			cRed, cReset, name, value, issuesURL)
	})
}

func (a *app) applyGuardSettings(cfg *config.Config) {
	a.hub.SetPinProtection(cfg.PinProtection.Enabled, cfg.PinProtection.PinHash)
	a.hub.SetAutoArmOnLock(cfg.AutoArmOnLock)
	if cfg.AutoArmOnLock {
		a.sensors.Enable("screen")
		a.sensors.StartEnabled()
		log.Info("Auto-arm on screen lock enabled — screen sensor started")
	}
}

func (a *app) wireCallbacks() {
	a.hub.SetAlarmTriggerCallback(a.alarm.Start)
	a.hub.SetAlarmDismissCallback(a.alarm.Stop)
	a.hub.SetAllDisconnectedCallback(a.reportNoPhoneConnected)
	a.hub.SetClientChangeCallback(a.clientsChanged)
}

// reportNoPhoneConnected says that an alert may not reach anybody.
//
// Monitoring carries on regardless — the machine is still being watched, and the
// event log still records what happened. What has gone is the part that reaches
// the owner, and that is worth saying rather than leaving them to notice.
func (a *app) reportNoPhoneConnected() {
	log.Warn("Every phone disconnected while armed — monitoring continues")
	a.sb.writeLine("  %s[LINK]%s No phone is connected. Monitoring continues; "+
		"an alert may not reach you until one reconnects.", cYellow, cReset)
}

// clientsChanged repaints the dashboard when a phone comes or goes. The count is
// on screen, and a stale one is the difference between "nothing is watching this
// for you" and "your phone is connected".
func (a *app) clientsChanged(int, bool) {
	a.sb.refresh()
}

// superviseLoops starts the long-lived work.
//
// Every one of these is supervised. A panic in any of them used to take the
// whole process down, which for an armed machine means the user walks back to a
// laptop that stopped watching itself and never said so.
func (a *app) superviseLoops(ctx context.Context, opts appOptions, deps consoleDeps) {
	safe.Supervise(ctx, "alert-dispatcher", a.hub.RunAlertDispatcher)
	safe.Supervise(ctx, "heartbeat", a.hub.RunHeartbeat)

	if opts.headless {
		return
	}
	// No terminal means no stdin to read commands from. -plain still has one:
	// what it gave up is the dashboard, not the person at the keyboard.
	safe.Supervise(ctx, "console", func(c context.Context) { runConsole(c, opts.in, deps) })

	if opts.plain {
		return
	}
	safe.Supervise(ctx, "status-ticker", func(c context.Context) { runStatusTicker(c, a.sb) })
	// Ctrl+Z has to give the terminal back before the process stops, and take
	// it again when the user brings it forward.
	safe.Supervise(ctx, "suspend", func(c context.Context) {
		watchSuspend(c, opts.out, a.sb.paint)
	})
	// A window that changed size has to be drawn for again, or every absolute
	// row the dashboard drew at points somewhere else.
	safe.Supervise(ctx, "resize", func(c context.Context) {
		watchResize(c, a.sb.paint)
	})
}

// superviseUpdateCheck asks GitHub for a newer release, repeatedly.
//
// Repeatedly rather than once at startup is the whole point: a copy installed as
// a service runs for weeks, and asking only at startup means the installations
// most in need of a fix are the least likely to hear about one. Nothing here can
// delay arming — it runs on its own supervised loop, behind its own timeout, and
// a failure goes no further than the debug log.
// A managed copy asks nothing. The desktop application ships the binary and
// decides which version it is, so a daily question to GitHub would have no
// action behind it and would offer an upgrade command that does not apply to
// this copy. SECURITY.md sets out what the check discloses; disclosing it for
// nothing is the part worth avoiding.
func (a *app) superviseUpdateCheck(ctx context.Context, cfg *config.Config,
	method update.Method, ledger *update.Ledger, managed bool,
) {
	if managed {
		return
	}

	a.sayWhatIsAlreadyKnown(ledger, method)

	watcher := update.Watcher{
		Interval: cfg.UpdateCheckInterval(),
		Settings: a.hub.UpdateSettings,
		Ledger:   ledger,
		Check:    checkForRelease,
		Report:   func(r update.Result) { a.reportUpdate(r, method) },
		OnError:  func(err error) { log.Debugf("Update check failed: %v", err) },
	}
	safe.Supervise(ctx, "update-check", watcher.Run)
}

// sayWhatIsAlreadyKnown repeats a release this installation has already found,
// at the moment somebody is looking at the screen.
//
// It asks nothing of the network. The ledger remembers the newest version ever
// reported, and the check that found it announced it once — into a log that has
// since scrolled, on a run that has since ended. Somebody restarting the program
// today would otherwise be told nothing about a release found yesterday, and the
// silence reads exactly like there being nothing to say.
//
// A version already installed says nothing, because IsNewer compares against
// what is running: upgrading is what ends the notice, not anybody clearing it.
func (a *app) sayWhatIsAlreadyKnown(ledger *update.Ledger, method update.Method) {
	if !update.IsRelease(version) {
		// And the one case where no check will ever run. Silence here is what
		// makes a development build look like a copy that is up to date.
		a.sb.writeLine("  %s[UPDATE]%s This is a development build (%s), so there is no version "+
			"to compare against and no check will run.", cDim, cReset, version)
		return
	}

	known := ledger.Load().LastSeenLatest
	if !update.IsNewer(known, version) {
		return
	}
	announceUpdate(a.sb, a.hub, update.Result{Available: true, Latest: known}, method)
}

// checkForRelease asks GitHub what the newest release on a channel is.
func checkForRelease(ctx context.Context, channel string) (update.Result, error) {
	return update.Checker{Channel: channel}.Check(ctx, version)
}

func (a *app) reportUpdate(r update.Result, method update.Method) {
	announceUpdate(a.sb, a.hub, r, method)
}

func (a *app) superviseBluetooth(ctx context.Context, cfg *config.Config) {
	if cfg.ConnectionMode != "bluetooth" && cfg.ConnectionMode != "both" {
		return
	}

	bleServer := ble.NewServer(a.hub)
	safe.Supervise(ctx, "ble-server", func(c context.Context) {
		a.reportBluetooth(bleServer.Start(c))
	})
}

// reportBluetooth says what became of the Bluetooth server.
//
// A platform that will not offer it is not a fault to be retried, and not
// something to bury in a log line the user will read as noise: they asked for
// Bluetooth and are not getting it. Say why, and say what still works.
func (a *app) reportBluetooth(err error) {
	switch {
	case err == nil:
	case errors.Is(err, ble.ErrNoCentralIdentity):
		a.sb.writeLine("  %s[BLE]%s Bluetooth pairing is off on this platform: %v.",
			cYellow, cReset, err)
		a.sb.writeLine("  %s      Pair over Wi-Fi instead — scan the QR code above.%s", cDim, cReset)
	default:
		log.Errorf("BLE server error: %v", err)
	}
}
