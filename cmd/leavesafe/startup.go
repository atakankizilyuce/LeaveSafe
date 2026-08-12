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
	"github.com/leavesafe/leavesafe/internal/eventlog"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/network"
	"github.com/leavesafe/leavesafe/internal/remote"
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
	remote  *remote.Controller

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

	a := &app{hub: hub, sensors: sensorMgr}
	a.openEventLog()

	// Read what the previous run left behind before anything can overwrite it.
	// If that run ended while armed, the machine has been unwatched ever since
	// and the user is about to be told so.
	stateStore := state.NewStore(config.ConfigDir(), version)
	prevState, prevStateErr := stateStore.Load()
	hub.SetStateStore(stateStore)

	a.openLogFile()

	// The local listener is plain HTTP and stays that way whether remote access
	// is on or off. It opens once here and is never closed again, which is what
	// lets the phone that turns remote access on stay connected while it
	// happens — remote access is a second listener rather than a different mode
	// for this one.
	a.srv = server.New(server.Config{Hub: hub, Port: listenPort(cfg.Port), DevMode: opts.devMode})
	if err := a.srv.Listen(); err != nil {
		return nil, fmt.Errorf("failed to bind port: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	remoteState := a.startRemoteAccess(ctx, cfg)
	// Announced to every connecting client, so a phone that arrived by QR code
	// can check it reached the server the code was printed for before it offers
	// the pairing key.
	hub.SetCertFingerprint(remoteState.CertFP)
	hub.SetRemoteState(remoteState)

	a.sb = a.drawInterface(opts, authMgr, remoteState, keyPath)
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
		srv: a.srv, remoteCtl: a.remote, cfg: cfg,
	})
	a.wireRemoteChanges(ctx)
	a.superviseUpdateCheck(ctx, cfg, installMethod, updateLedger)
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
	fmt.Printf("  %sShutting down…%s\n", cDim, cReset)

	a.cancel()
	a.alarm.Stop()
	a.sensors.StopAll()
	// Closes the remote listener and takes the router's port mapping back down
	// with it. Leaving a mapping behind would leave a hole in the router
	// pointing at a machine that is no longer listening.
	a.remote.Disable()
	if el := a.hub.EventLogger(); el != nil {
		_ = el.Close()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := a.srv.Shutdown(shutdownCtx); err != nil {
		log.Warnf("Server shutdown: %v", err)
	}
}

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

// startRemoteAccess builds the remote controller and brings it up if the config
// asks for it, returning whatever state that left it in.
func (a *app) startRemoteAccess(ctx context.Context, cfg *config.Config) remote.State {
	a.remote = remote.NewController(a.srv, config.ConfigDir(), cfg.RemotePort, remote.Deps{
		Cert: server.GenerateOrLoadCert,
		OpenPort: func(p int) (remote.PortMapping, error) {
			return network.OpenPort(p)
		},
		PublicIP: network.GetPublicIP,
	})

	if cfg.RemoteAccess != nil && *cfg.RemoteAccess {
		if st := a.remote.Enable(ctx); st.Reason != "" {
			log.Warn(st.Reason)
		}
	}
	return a.remote.State()
}

// drawInterface puts the program on screen, or into the log when there is no
// screen to put it on.
func (a *app) drawInterface(opts appOptions, authMgr *auth.Manager,
	remoteState remote.State, keyPath string,
) *statusBar {
	if opts.headless || opts.plain {
		sb := newHeadlessStatusBar(a.hub, a.sensors, authMgr.PairingKey(), authMgr.RawPairingKey(),
			reachableURLs(a.srv, remoteState), remoteState.CertFP, keyPath)
		mode := "headless"
		if opts.plain {
			mode = "without the dashboard"
		}
		logHeadlessStartup(sb, a.srv, remoteState.CertFP, mode)
		if opts.plain {
			// Somebody is watching this one, and without a dashboard there is
			// nowhere else a code to scan could appear. Printed rather than
			// positioned, so it scrolls away with everything else.
			printPairingCode(opts.out, sb, authMgr.RawPairingKey(), remoteState.CertFP)
		}
		return sb
	}

	sb := buildDashboard(opts.out, a.srv, authMgr, a.hub, a.sensors, remoteState)
	// The dashboard owns the terminal, so log lines have to be routed through it
	// to land inside its scrolling region rather than on top of the QR code.
	// Headless has no such constraint.
	log.SetOutput(&logWriter{sb: sb})
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
}

// wireRemoteChanges routes every later change to remote access onto the
// dashboard and every paired phone.
func (a *app) wireRemoteChanges(ctx context.Context) {
	// The connection mode can be changed from the phone or from the console
	// while the program runs. Both end here, and both end by pushing the result
	// onto the dashboard and every paired phone.
	//
	// Disabling first rather than branching makes it idempotent for the enable
	// case too: a change of remote_port arrives as the same signal and has to
	// rebind rather than return the port already in use.
	a.hub.SetRemoteToggle(func(enable bool) { a.setRemoteAccess(ctx, enable) })

	// Reachability arrives on its own, up to a minute after Enable returned, and
	// it has to land on the dashboard and every paired phone the same way a
	// change made from the console does.
	a.remote.SetOnUpdate(a.publishRemoteState)

	// The startup state has to reach the dashboard the same way a later change
	// does, or the first draw and every draw after it would come from different
	// code.
	a.publishRemoteState(a.remote.State())
}

// setRemoteAccess turns remote access on or off.
//
// Disabling first rather than branching makes it idempotent for the enable case
// too: a change of remote_port arrives as the same signal and has to rebind
// rather than return the port already in use.
func (a *app) setRemoteAccess(ctx context.Context, enable bool) {
	st := a.remote.Disable()
	if enable {
		st = a.remote.Enable(ctx)
	}
	a.publishRemoteState(st)
}

// publishRemoteState puts one state onto the dashboard and every paired phone.
func (a *app) publishRemoteState(st remote.State) {
	applyRemoteState(a.sb, a.hub, a.srv, st)
}

// superviseUpdateCheck asks GitHub for a newer release, repeatedly.
//
// Repeatedly rather than once at startup is the whole point: a copy installed as
// a service runs for weeks, and asking only at startup means the installations
// most in need of a fix are the least likely to hear about one. Nothing here can
// delay arming — it runs on its own supervised loop, behind its own timeout, and
// a failure goes no further than the debug log.
func (a *app) superviseUpdateCheck(ctx context.Context, cfg *config.Config,
	method update.Method, ledger *update.Ledger,
) {
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
