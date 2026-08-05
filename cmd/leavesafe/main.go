package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/leavesafe/leavesafe/internal/alarm"
	"github.com/leavesafe/leavesafe/internal/auth"
	ble "github.com/leavesafe/leavesafe/internal/bluetooth"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/eventlog"
	"github.com/leavesafe/leavesafe/internal/location"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/network"
	"github.com/leavesafe/leavesafe/internal/qr"
	"github.com/leavesafe/leavesafe/internal/remote"
	"github.com/leavesafe/leavesafe/internal/safe"
	"github.com/leavesafe/leavesafe/internal/server"
	"github.com/leavesafe/leavesafe/internal/state"
	"github.com/leavesafe/leavesafe/internal/update"
	"github.com/leavesafe/leavesafe/internal/ws"
)

var version = "dev"

// repoURL is where the project lives; the CLI points at it for bug reports and
// release downloads rather than repeating the address in several places.
const (
	repoURL   = "https://github.com/atakankizilyuce/LeaveSafe"
	issuesURL = repoURL + "/issues"
)

// eventLogFileName is the security event history kept in the config directory.
const eventLogFileName = "events.jsonl"

// clockFormat is how a time of day is written wherever one is shown to the
// person watching: the log timestamps, the armed-since line, the event list.
// The date is deliberately absent — everything here happened during the session
// on screen.
const clockFormat = "15:04:05"

// clockTime writes a moment as the time of day alone. Everything the console
// shows happened during the session in front of you, so the date would be the
// same on every line.
func clockTime(t time.Time) string {
	return t.Format(clockFormat)
}

const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cCyan   = "\033[36m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cRed    = "\033[31m"
)

type statusBar struct {
	mu        sync.Mutex
	hub       *ws.Hub
	sensorMgr *monitor.Manager
	out       io.Writer

	gridRow   int
	gridCol   int
	gridWidth int

	// QR geometry. The box is sized to the largest QR of any candidate URL so
	// that switching between them with `qr <n>` never overflows the layout.
	qrRow    int
	qrCol    int
	qrBoxW   int
	qrBoxH   int
	qrCodes  [][]string
	qrURLIdx int

	key string
	// rawKey is the pairing key as it goes into a QR code, without the grouping
	// that makes `key` readable on screen. The two are kept side by side
	// because the URL list is rebuilt whenever remote access comes or goes, and
	// rebuilding a QR code needs the raw form.
	rawKey       string
	urls         []string
	remoteStatus string // e.g. "ACTIVE — 85.1.2.3:9443" or "" if not remote
	certFP       string // SHA-256 fingerprint of the TLS certificate, "" if plain HTTP

	// headless drops every drawing operation. Started from an autostart entry
	// there is no terminal to draw on: the cursor-positioning escapes would go
	// into a log file as garbage, and the QR code would be a wall of blocks
	// nobody ever sees. Messages become ordinary log lines instead.
	headless bool
	// keyPath is where the pairing key is persisted, empty when it is not.
	// Rotating the key has to update that file, or the next start would come
	// back with the key the user just invalidated.
	keyPath string
}

// newHeadlessStatusBar returns a status bar that draws nothing, for runs with
// no terminal attached.
func newHeadlessStatusBar(hub *ws.Hub, sensorMgr *monitor.Manager, key, rawKey string,
	urls []string, certFP, keyPath string,
) *statusBar {
	return &statusBar{
		out:       io.Discard,
		hub:       hub,
		sensorMgr: sensorMgr,
		key:       key,
		rawKey:    rawKey,
		urls:      urls,
		certFP:    certFP,
		headless:  true,
		keyPath:   keyPath,
		qrURLIdx:  -1,
	}
}

// setURLs replaces the addresses the dashboard offers and rebuilds their QR
// codes.
//
// The list is not fixed for the life of the process any more: turning remote
// access on adds a public URL and turning it off removes one. A stale QR code
// is worse than none — it is an address the user will scan and then wait at.
func (sb *statusBar) setURLs(urls []string) {
	sb.mu.Lock()
	rawKey, certFP := sb.rawKey, sb.certFP
	sb.mu.Unlock()

	codes := make([][]string, 0, len(urls))
	for _, u := range urls {
		lines, err := qr.Lines(pairingURL(u, rawKey, certFP))
		if err != nil {
			log.Warnf("Could not render QR code for %s: %v", u, err)
			lines = nil
		}
		codes = append(codes, lines)
	}

	sb.mu.Lock()
	sb.urls = urls
	sb.qrCodes = codes
	if sb.qrURLIdx >= len(urls) {
		sb.qrURLIdx = 0
	}
	sb.mu.Unlock()

	sb.refresh()
}

// urlList returns the addresses currently on offer.
func (sb *statusBar) urlList() []string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return append([]string(nil), sb.urls...)
}

// setRemoteStatus replaces the dashboard's remote-access line.
func (sb *statusBar) setRemoteStatus(status string) {
	sb.mu.Lock()
	sb.remoteStatus = status
	sb.mu.Unlock()
	sb.refresh()
}

// setCertFP records the fingerprint the QR codes and the header should carry.
// It changes when remote access comes up or goes away, because the certificate
// belongs to that listener.
func (sb *statusBar) setCertFP(fp string) {
	sb.mu.Lock()
	sb.certFP = fp
	sb.mu.Unlock()
}

func visLen(s string) int {
	n, i := 0, 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		n++
		i += size
	}
	return n
}

func (sb *statusBar) boxLine(content string) string {
	inner := sb.gridWidth - 2
	pad := inner - visLen(content)
	if pad < 0 {
		pad = 0
	}
	return "│" + content + strings.Repeat(" ", pad) + "│"
}

func (sb *statusBar) gridLines() []string {
	armedLabel := cDim + "DISARMED" + cReset
	armedDot := cDim + "●" + cReset
	if sb.hub.IsArmed() {
		armedLabel = cRed + cBold + "● ARMED" + cReset
		armedDot = cRed + "●" + cReset
	}

	clients := sb.hub.ClientCount()

	sensors := sb.sensorMgr.Sensors()
	total, active := len(sensors), 0
	for _, s := range sensors {
		// A sensor whose loop has failed is not active, however it is
		// configured. This count is what the user reads before walking away.
		//
		// AvailableNow, because this runs on the five-second repaint and holds
		// the lock the log lines go through: a sensor that answers by starting
		// PowerShell would otherwise freeze the dashboard while it thought.
		available, _ := sb.sensorMgr.AvailableNow(s)
		if sb.sensorMgr.IsEnabled(s.Name()) && available && sb.sensorMgr.Failure(s.Name()) == "" {
			active++
		}
	}

	w := sb.gridWidth
	top := "┌" + strings.Repeat("─", w-2) + "┐"
	midSep := "├" + strings.Repeat("─", w-2) + "┤"
	bottom := "└" + strings.Repeat("─", w-2) + "┘"

	lines := []string{
		top,
		sb.boxLine("  " + cBold + cCyan + "◉  STATUS" + cReset),
		midSep,
		sb.boxLine(fmt.Sprintf("  %s  State    %s", armedDot, armedLabel)),
		sb.boxLine(fmt.Sprintf("  %s●%s  Clients  %s%d%s connected", cGreen, cReset, cBold, clients, cReset)),
		sb.boxLine(fmt.Sprintf("  %s●%s  Sensors  %s%d / %d%s active", cCyan, cReset, cBold, active, total, cReset)),
	}

	if sb.remoteStatus != "" {
		lines = append(lines, sb.boxLine(fmt.Sprintf("  %s●%s  Remote   %s%s%s", cGreen, cReset, cGreen, sb.remoteStatus, cReset)))
	}

	// The certificate is self-signed, so the phone will warn about it. Showing
	// the fingerprint is what lets the user tell that warning apart from an
	// actual interception. `cert` prints it in full.
	if sb.certFP != "" {
		lines = append(lines, sb.boxLine(fmt.Sprintf("  %s●%s  Cert     %s%s…%s", cCyan, cReset, cDim, shortFingerprint(sb.certFP), cReset)))
	}

	lines = append(lines, midSep)
	lines = append(lines, sb.boxLine(fmt.Sprintf("  %s●%s  Key      %s%s%s", cYellow, cReset, cBold, sb.key, cReset)))

	maxURLVis := w - 2 - visLen("  ●  URL      ")
	for _, url := range sb.urls {
		if utf8.RuneCountInString(url) > maxURLVis {
			runes := []rune(url)
			url = string(runes[:maxURLVis-3]) + "..."
		}
		lines = append(lines, sb.boxLine(
			fmt.Sprintf("  %s●%s  URL      %s%s%s", cGreen, cReset, cGreen, url, cReset),
		))
	}

	lines = append(lines, bottom)
	return lines
}

// shortFingerprint returns the first four colon-separated octets of a SHA-256
// fingerprint, which is enough to eyeball against the phone's warning dialog.
func shortFingerprint(fp string) string {
	parts := strings.Split(fp, ":")
	if len(parts) > 4 {
		parts = parts[:4]
	}
	return strings.Join(parts, ":")
}

// drawQR repaints the reserved QR box with the currently selected code. The
// box is cleared first because codes for different URLs differ in size.
func (sb *statusBar) drawQR() {
	if sb.headless || sb.qrBoxH == 0 {
		return
	}
	blank := strings.Repeat(" ", sb.qrBoxW)
	for i := 0; i < sb.qrBoxH; i++ {
		fmt.Fprintf(sb.out, "\033[%d;%dH%s", sb.qrRow+i, sb.qrCol, blank)
	}
	if sb.qrURLIdx < 0 || sb.qrURLIdx >= len(sb.qrCodes) {
		return
	}
	for i, line := range sb.qrCodes[sb.qrURLIdx] {
		if i >= sb.qrBoxH {
			break
		}
		fmt.Fprintf(sb.out, "\033[%d;%dH%s", sb.qrRow+i, sb.qrCol, line)
	}
}

// showQR switches the displayed QR code to the URL at index i (1-based, as the
// dashboard lists them). Returns the URL shown, or "" if the index is unknown.
func (sb *statusBar) showQR(i int) string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.headless || i < 1 || i > len(sb.qrCodes) {
		return ""
	}
	sb.qrURLIdx = i - 1
	fmt.Fprintf(sb.out, "\033[s")
	sb.drawQR()
	fmt.Fprintf(sb.out, "\033[u")
	return sb.urls[sb.qrURLIdx]
}

// rekeyQR re-renders every QR code against a new pairing key. Without this the
// codes on screen keep encoding the key that `rotate-key` just invalidated.
func (sb *statusBar) rekeyQR(rawKey string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.headless {
		return
	}
	for i, u := range sb.urls {
		lines, err := qr.Lines(pairingURL(u, rawKey, sb.certFP))
		if err != nil {
			continue
		}
		sb.qrCodes[i] = lines
	}
	fmt.Fprintf(sb.out, "\033[s")
	sb.drawQR()
	fmt.Fprintf(sb.out, "\033[u")
}

func (sb *statusBar) doRedrawGrid() {
	if sb.headless {
		return
	}
	lines := sb.gridLines()
	fmt.Fprintf(sb.out, "\033[s")
	for i, line := range lines {
		fmt.Fprintf(sb.out, "\033[%d;%dH%s", sb.gridRow+i, sb.gridCol, line)
	}
	fmt.Fprintf(sb.out, "\033[u")
}

func (sb *statusBar) refresh() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.doRedrawGrid()
}

func (sb *statusBar) writeLine(format string, args ...interface{}) {
	if sb.headless {
		// The escape codes and box drawing mean nothing in a log file. Strip
		// the layout and let the logger own the line.
		log.Info(strings.TrimSpace(stripANSI(fmt.Sprintf(format, args...))))
		return
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()
	fmt.Fprintf(sb.out, "%s\n", fmt.Sprintf(format, args...))
	sb.doRedrawGrid()
}

// stripANSI removes the color and cursor escapes the dashboard uses, so the
// same message can be written to a log file.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' && s[i] != 'H' {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

type logWriter struct{ sb *statusBar }

func (w *logWriter) Write(p []byte) (n int, err error) {
	if msg := strings.TrimRight(string(p), "\n"); msg != "" {
		w.sb.writeLine("  %s", msg)
	}
	return len(p), nil
}

// consoleLogFormatter is how log lines are stamped in the dashboard. It is its
// own function so the choice can be asserted on without starting the program.
func consoleLogFormatter() *log.TextFormatter {
	return &log.TextFormatter{
		TimestampFormat: clockFormat,
		FullTimestamp:   true,
	}
}

func main() {
	devMode := flag.Bool("dev", false, "serve web assets from filesystem for live reload")
	showVersion := flag.Bool("version", false, "print the version and exit")
	headless := flag.Bool("headless", false, "run without the terminal dashboard, for autostart")
	flag.Usage = printUsage
	flag.Parse()

	if *showVersion {
		fmt.Println(versionLine())
		return
	}

	// Subcommands run and exit without starting the monitor: they administer
	// the installation rather than being part of it.
	if args := flag.Args(); len(args) > 0 {
		os.Exit(runSubcommand(args))
	}

	log.SetFormatter(consoleLogFormatter())

	if !*headless {
		maximizeConsole()
		time.Sleep(200 * time.Millisecond)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Warnf("Failed to load config, using defaults: %v", err)
	}

	// A hand-edited config is the normal way to change most of these settings,
	// and a typo should not produce a monitor that never heartbeats.
	for _, note := range cfg.Validate() {
		log.Warnf("Config: %s", note)
	}

	// The connection mode is asked on every interactive start, with the saved
	// value as the default, so a user who changed it from their phone sees what
	// is in force and can change it back without hunting for the setting.
	//
	// A headless start has nobody to answer and blocking on stdin there would
	// hang the service forever, so it takes what is stored. Every autostart
	// entry this program writes passes -headless, so no unattended start can
	// reach the prompt.
	if *headless {
		if cfg.RemoteAccess == nil {
			local := false
			cfg.RemoteAccess = &local
			if err := config.Save(cfg); err != nil {
				log.Warnf("Failed to save config: %v", err)
			}
		}
		log.Infof("Connection mode: %s (from config, no terminal to ask)",
			connectionModeName(*cfg.RemoteAccess))
	} else {
		// Language first, and only once. It decides how the next question is
		// worded, so there is no order in which it could come second.
		promptRemoteAccess(cfg, ensureLanguage(cfg))
	}

	// Migrate a cleartext PIN left by an older version: hash it and rewrite the
	// config so the digits themselves no longer live on disk.
	if cfg.PinProtection.Pin != "" {
		if hash, err := auth.HashPin(cfg.PinProtection.Pin); err != nil {
			log.Warnf("Failed to hash stored PIN: %v", err)
		} else {
			cfg.PinProtection.PinHash = hash
			cfg.PinProtection.Pin = ""
			if err := config.Save(cfg); err != nil {
				log.Warnf("Failed to save config after PIN migration: %v", err)
			}
		}
	}

	remoteEnabled := cfg.RemoteAccess != nil && *cfg.RemoteAccess

	authOpts := auth.Options{
		MaxSessions:   cfg.MaxSessions,
		MaxAttempts:   cfg.MaxAuthAttempts,
		LockoutPeriod: time.Duration(cfg.LockoutSeconds) * time.Second,
		SessionTTL:    time.Duration(cfg.SessionTTLMinutes) * time.Minute,
		SessionIdle:   time.Duration(cfg.SessionIdleMinutes) * time.Minute,
	}

	// A headless start has no screen to show a QR code on, so a freshly
	// generated key would be a secret nobody could read — and a phone paired
	// before the reboot would be locked out by a key it never saw. The
	// persisted key is what makes the pairing survive the restart.
	keyPath := ""
	if *headless {
		keyPath = filepath.Join(config.ConfigDir(), auth.KeyFileName)
		persisted, err := auth.LoadOrCreateKeyFile(keyPath)
		if err != nil {
			log.Fatalf("Failed to prepare the stored pairing key: %v", err)
		}
		authOpts.PairingKey = persisted
	}

	authMgr, err := auth.NewManagerWithOptions(authOpts)
	if err != nil {
		log.Fatalf("Failed to initialize auth: %v", err)
	}

	sensorMgr := monitor.NewManager()
	registerSensors(sensorMgr, cfg)

	hub := ws.NewHub(authMgr, sensorMgr, version)
	hub.SetConfig(cfg)
	hub.SetTimings(
		time.Duration(cfg.HeartbeatSeconds)*time.Second,
		time.Duration(cfg.DisconnectGraceSeconds)*time.Second,
	)

	evLogPath := filepath.Join(config.ConfigDir(), eventLogFileName)
	if err := os.MkdirAll(config.ConfigDir(), 0o700); err != nil {
		log.Warnf("Failed to create config dir: %v", err)
	}
	if evLog, err := eventlog.New(evLogPath); err != nil {
		log.Warnf("Failed to open event log: %v", err)
	} else {
		hub.SetEventLogger(evLog)
		defer evLog.Close()
	}

	// Read what the previous run left behind before anything can overwrite it.
	// If that run ended while armed, the machine has been unwatched ever since
	// and the user is about to be told so.
	stateStore := state.NewStore(config.ConfigDir(), version)
	prevState, prevStateErr := stateStore.Load()
	hub.SetStateStore(stateStore)

	// The terminal log scrolls away and the window eventually closes. The file
	// is what is left to answer "why did nothing happen last Tuesday".
	logHook, err := newFileHook(config.ConfigDir())
	if err != nil {
		log.Warnf("Failed to open the log file, logging to the terminal only: %v", err)
	} else {
		log.AddHook(logHook)
		defer logHook.Close()
	}

	port := cfg.Port
	if v := os.Getenv("PORT"); v != "" {
		parsed, err := strconv.Atoi(v)
		switch {
		case err != nil:
			// Silently falling back used to mean the server came up on a port
			// the user did not ask for, with a QR code they would scan and
			// wonder about. Say what happened instead.
			log.Warnf("Ignoring PORT=%q: not a number. Using port %d from the config.", v, port)
		case parsed < 0 || parsed > 65535:
			log.Warnf("Ignoring PORT=%d: outside the valid range 0-65535. Using port %d from the config.", parsed, port)
		default:
			port = parsed
		}
	}

	// The local listener is plain HTTP and stays that way whether remote access
	// is on or off. It opens once here and is never closed again, which is what
	// lets the phone that turns remote access on stay connected while it
	// happens — remote access is a second listener rather than a different mode
	// for this one.
	srv := server.New(server.Config{Hub: hub, Port: port, DevMode: *devMode})
	if err := srv.Listen(); err != nil {
		log.Fatalf("Failed to bind port: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	remoteCtl := remote.NewController(srv, config.ConfigDir(), cfg.RemotePort, remote.Deps{
		Cert: server.GenerateOrLoadCert,
		OpenPort: func(p int) (remote.PortMapping, error) {
			return network.OpenPort(p)
		},
		PublicIP: network.GetPublicIP,
	})
	if remoteEnabled {
		if st := remoteCtl.Enable(ctx); st.Reason != "" {
			log.Warn(st.Reason)
		}
	}
	remoteState := remoteCtl.State()
	certFP := remoteState.CertFP
	// Announced to every connecting client, so a phone that arrived by QR code
	// can check it reached the server the code was printed for before it offers
	// the pairing key.
	hub.SetCertFingerprint(certFP)
	hub.SetRemoteState(remoteState)

	var sb *statusBar
	if *headless {
		sb = newHeadlessStatusBar(hub, sensorMgr, authMgr.PairingKey(), authMgr.RawPairingKey(),
			reachableURLs(srv, remoteState), certFP, keyPath)
		logHeadlessStartup(sb, srv, certFP)
	} else {
		sb = buildDashboard(os.Stdout, srv, authMgr, hub, sensorMgr, remoteState)
		// The dashboard owns the terminal, so log lines have to be routed
		// through it to land inside its scrolling region rather than on top of
		// the QR code. Headless has no such constraint.
		log.SetOutput(&logWriter{sb: sb})
	}

	// A recovered panic is still a bug, and one that happened while the machine
	// was supposed to be watching itself. safe already logged the stack; this
	// puts it in the event history next to arm and disarm, and on the dashboard
	// where the user will actually see it.
	safe.SetPanicHandler(func(name string, value any, _ []byte) {
		if el := hub.EventLogger(); el != nil {
			el.Log(eventlog.Event{
				Type:    eventlog.EventPanic,
				Sensor:  name,
				Message: fmt.Sprintf("Recovered from a panic in %s: %v", name, value),
			})
		}
		sb.writeLine("  %s[BUG]%s Recovered from a panic in %s: %v — please report this at %s",
			cRed, cReset, name, value, issuesURL)
	})

	localAlarm := alarm.New(cfg.Alarm)

	hub.SetPinProtection(cfg.PinProtection.Enabled, cfg.PinProtection.PinHash)
	hub.SetAutoArmOnLock(cfg.AutoArmOnLock)
	if cfg.AutoArmOnLock {
		sensorMgr.Enable("screen")
		sensorMgr.StartEnabled()
		log.Info("Auto-arm on screen lock enabled — screen sensor started")
	}

	reportInterruptedMonitoring(sb, hub, stateStore, prevState, prevStateErr, cfg.RestoreArmedState)

	// How this copy was installed decides what the user is told to run. Resolved
	// once: the binary does not move while it is running.
	installMethod := update.DetectSelf()
	updateLedger := update.NewLedger(config.ConfigDir())

	hub.SetAlarmTriggerCallback(func() {
		localAlarm.Start()
	})
	hub.SetAlarmDismissCallback(func() {
		localAlarm.Stop()
	})
	hub.SetAllDisconnectedCallback(func() {
		log.Warn("Every phone disconnected while armed — monitoring continues")
		sb.writeLine("  %s[LINK]%s No phone is connected. Monitoring continues; "+
			"an alert may not reach you until one reconnects.", cYellow, cReset)
	})
	hub.SetClientChangeCallback(func(_ int, _ bool) {
		sb.refresh()
	})

	hub.SetLocationTracker(ctx, buildLocationTracker(cfg))

	// Which sensors can work here is reported once, in the background, because
	// finding out can be slow and nothing about serving should wait on it.
	safe.Go("sensor-availability", func() { logSensorAvailability(sensorMgr) })

	// Every long-lived loop is supervised. A panic in any one of them used to
	// take the whole process down, which for an armed machine means the user
	// walks back to a laptop that stopped watching itself and never said so.
	safe.Supervise(ctx, "alert-dispatcher", hub.RunAlertDispatcher)
	safe.Supervise(ctx, "heartbeat", hub.RunHeartbeat)
	if !*headless {
		safe.Supervise(ctx, "status-ticker", func(c context.Context) { runStatusTicker(c, sb) })
		// No terminal means no stdin to read commands from.
		safe.Supervise(ctx, "console", func(c context.Context) {
			runConsole(c, os.Stdin, consoleDeps{hub: hub, sb: sb, localAlarm: localAlarm,
				authMgr: authMgr, installMethod: installMethod, updateLedger: updateLedger,
				srv: srv, remoteCtl: remoteCtl, cfg: cfg})
		})
	}

	// The connection mode can be changed from the phone or from the console
	// while the program runs. Both end here, and both end by pushing the result
	// onto the dashboard and every paired phone.
	//
	// Disabling first rather than branching makes it idempotent for the enable
	// case too: a change of remote_port arrives as the same signal and has to
	// rebind rather than return the port already in use.
	hub.SetRemoteToggle(func(enable bool) {
		st := remoteCtl.Disable()
		if enable {
			st = remoteCtl.Enable(ctx)
		}
		applyRemoteState(sb, hub, srv, st)
	})

	// Reachability arrives on its own, up to a minute after Enable returned, and
	// it has to land on the dashboard and every paired phone the same way a
	// change made from the console does.
	remoteCtl.SetOnUpdate(func(st remote.State) {
		applyRemoteState(sb, hub, srv, st)
	})

	// The startup state has to reach the dashboard the same way a later change
	// does, or the first draw and every draw after it would come from different
	// code.
	applyRemoteState(sb, hub, srv, remoteCtl.State())

	// Checking repeatedly, rather than once at startup, is the whole point: a copy
	// installed as a service runs for weeks, and asking only at startup means the
	// installations most in need of a fix are the least likely to hear about one.
	// Nothing here can delay arming — it runs on its own supervised loop, behind
	// its own timeout, and a failure goes no further than the debug log.
	safe.Supervise(ctx, "update-check", func(c context.Context) {
		update.Watcher{
			Interval: cfg.UpdateCheckInterval(),
			Settings: hub.UpdateSettings,
			Ledger:   updateLedger,
			Check: func(checkCtx context.Context, channel string) (update.Result, error) {
				return update.Checker{Channel: channel}.Check(checkCtx, version)
			},
			Report: func(r update.Result) {
				announceUpdate(sb, hub, r, installMethod)
			},
			OnError: func(err error) { log.Debugf("Update check failed: %v", err) },
		}.Run(c)
	})

	connMode := cfg.ConnectionMode
	if connMode == "bluetooth" || connMode == "both" {
		bleServer := ble.NewServer(hub)
		safe.Supervise(ctx, "ble-server", func(c context.Context) {
			err := bleServer.Start(c)
			switch {
			case err == nil:
			case errors.Is(err, ble.ErrNoCentralIdentity):
				// Not a fault to be retried, and not something to bury in a
				// log line the user will read as noise: they asked for
				// Bluetooth and are not getting it. Say why, and say what
				// still works.
				sb.writeLine("  %s[BLE]%s Bluetooth pairing is off on this platform: %v.",
					cYellow, cReset, err)
				sb.writeLine("  %s      Pair over Wi-Fi instead — scan the QR code above.%s", cDim, cReset)
			default:
				log.Errorf("BLE server error: %v", err)
			}
		})
	}

	cleanup := func() {
		fmt.Fprintf(os.Stdout, "\033[r\033[?25h\n")
		fmt.Printf("  %sShutting down…%s\n", cDim, cReset)
		cancel()
		localAlarm.Stop()
		sensorMgr.StopAll()
		// Closes the remote listener and takes the router's port mapping back
		// down with it. Leaving a mapping behind would leave a hole in the
		// router pointing at a machine that is no longer listening.
		remoteCtl.Disable()
		if el := hub.EventLogger(); el != nil {
			_ = el.Close()
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Warnf("Server shutdown: %v", err)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	safe.Go("shutdown", func() {
		<-sigCh
		cleanup()
		os.Exit(0)
	})

	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// pairingURL builds the address a QR code encodes: the server, the pairing key,
// and — when there is a certificate — its fingerprint.
//
// Both ride in the fragment, after the '#', and that is the whole point. A
// fragment is never put on the wire: the browser strips it before building the
// request, so the key reaches the page's own JavaScript and nothing else. It
// used to be a query parameter, which meant the very first request line the
// phone sent carried the key in clear — to whatever server answered that
// address, before a single byte of the page had run. The check that holds the
// key back until the server names its certificate was then guarding a secret
// that had already left, and on the default plain-HTTP path it left in clear on
// the café Wi-Fi.
//
// The fingerprint rides along so the phone can check which server it reached
// before it offers the key, and so the value is on the phone's own screen to
// compare against the browser's certificate warning. Previously it was only on
// the laptop, which is the one place nobody looks while holding a phone. The
// colons are dropped to keep the QR code small; both ends compare on the hex.
//
// This does not turn a self-signed certificate into a verified one. See
// SECURITY.md for what it does and does not catch.
func pairingURL(base, rawKey, certFP string) string {
	fragment := "key=" + rawKey
	if certFP != "" {
		fragment += "&fp=" + strings.ToLower(strings.ReplaceAll(certFP, ":", ""))
	}
	return base + "/#" + fragment
}

// reachableURLs returns every address a phone could connect to.
//
// The order is the whole point, because the dashboard draws the first of these
// as a QR code and that is the one the user scans. It goes first only when the
// router accepted a port mapping — when it did not, the public address is a
// hope rather than a route: the machine has an address on the internet and
// nothing on the path will carry a connection to it. Offering that as the code
// to scan is how a phone ends up on a spinner forever, which is exactly what
// happened on a network with no UPnP gateway.
//
// It stays in the list rather than disappearing, because the accompanying
// message tells the user to forward the port by hand and it has to be there to
// scan once they have. `urls` lists it and `qr <n>` shows it.
func reachableURLs(srv *server.Server, st remote.State) []string {
	urls := srv.URLs()
	if st.PublicURL == "" {
		return urls
	}
	if st.UPnP != remote.UPnPOK {
		return append(urls, st.PublicURL)
	}
	return append([]string{st.PublicURL}, urls...)
}

// applyRemoteState is what every path that changes the connection mode ends
// with: the addresses on the dashboard, the certificate the QR codes carry, the
// status the phones hold, and the reason if there is one, all from the same
// State.
func applyRemoteState(sb *statusBar, hub *ws.Hub, srv *server.Server, st remote.State) {
	sb.setCertFP(st.CertFP)
	sb.setURLs(reachableURLs(srv, st))
	sb.setRemoteStatus(remoteStatusLine(st))
	hub.SetCertFingerprint(st.CertFP)
	hub.SetRemoteState(st)
	if st.Reason != "" {
		sb.writeLine("  %s[NET]%s %s", cYellow, cReset, st.Reason)
	}
}

// remoteStatusLine is the dashboard's one-line summary of remote access.
func remoteStatusLine(st remote.State) string {
	switch {
	case !st.Enabled && st.UPnP == remote.UPnPCarrierNAT:
		return "OFF — carrier-grade NAT"
	case !st.Enabled:
		return ""
	case st.Probing:
		// Named, because the wait is long enough to look like a hang otherwise:
		// a network with no UPnP gateway takes about thirty-five seconds to say
		// so, and "public address unknown" during that is a wrong answer rather
		// than an early one.
		return "ON — checking whether it can be reached…"
	case st.UPnP == remote.UPnPFailed:
		return fmt.Sprintf("ON — not reachable yet, forward TCP %d by hand", st.ManualPort)
	case st.PublicURL == "":
		return "ACTIVE — public address unknown"
	default:
		return "ACTIVE — " + strings.TrimPrefix(st.PublicURL, "https://")
	}
}

// logHeadlessStartup writes what the dashboard would have shown.
//
// Started from an autostart entry there is no screen and no QR code, so the log
// file is the only place the user can find the address to open and the
// certificate to check. Not the pairing key: that is in its own owner-only file
// rather than in a log that gets pasted into bug reports.
func logHeadlessStartup(sb *statusBar, srv *server.Server, certFP string) {
	log.Infof("LeaveSafe %s started headless — no dashboard on this run", version)
	for _, u := range sb.urls {
		log.Infof("Reachable at %s", u)
	}
	if certFP != "" {
		log.Infof("TLS certificate SHA-256: %s", certFP)
	} else if srv.IsTLS() {
		log.Info("TLS is on but the certificate fingerprint is unknown")
	}
	if sb.keyPath != "" {
		log.Infof("Pairing key is stored in %s — it is not written to this log", sb.keyPath)
	}
}

// announceUpdate says a newer release exists, on the dashboard and on the phone.
//
// Nothing is downloaded and nothing is replaced: a security program that
// silently rewrites itself from the network is a larger trust decision than the
// user made when they downloaded a single file. But a single file is also a
// file nothing updates, and the whole point of hearing about a fix is hearing
// about it before you need it.
//
// The phone matters more than the terminal here. A copy started by
// `install-service` runs headless, where this line goes to leavesafe.log and
// nobody reads it — while the phone is the screen the user actually carries.
func announceUpdate(sb *statusBar, hub *ws.Hub, result update.Result, method update.Method) {
	_, channel := hub.UpdateSettings()
	channel = update.NormalizeChannel(channel)

	url := result.URL
	if url == "" {
		url = repoURL + "/releases/latest"
	}
	command := method.Command()

	sb.writeLine("  %s[UPDATE]%s %s is available — you are running %s", cCyan, cReset, result.Latest, version)
	if command != "" {
		sb.writeLine("  %s         %s%s", cBold, command, cReset)
	} else {
		sb.writeLine("  %s         %s%s", cDim, url, cReset)
	}
	sb.writeLine("  %s         Set \"update_check\": false in the config to stop checking.%s", cDim, cReset)

	hub.SetUpdateAvailable(ws.UpdatePayload{
		Running: version,
		Latest:  result.Latest,
		URL:     url,
		Channel: channel,
		Command: command,
	})
}

// checkForUpdateNow runs a check because the user asked for one, and reports the
// answer either way.
//
// Being asked is different from the scheduled path in two ways: it ignores when
// the last check ran, and it reports even a version already reported. It also
// says when there is nothing to report, which the scheduled path never does — a
// background line saying "still up to date" every day would be noise, but the
// answer to a question is not.
func checkForUpdateNow(sb *statusBar, hub *ws.Hub, method update.Method, ledger *update.Ledger) {
	enabled, channel := hub.UpdateSettings()
	channel = update.NormalizeChannel(channel)
	if !enabled {
		sb.writeLine("  Update checking is off. Set %s\"update_check\": true%s in the config to turn it back on.", cBold, cReset)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	sb.writeLine("  Checking for updates on the %s%s%s channel…", cBold, channel, cReset)
	result, err := update.Checker{Channel: channel}.Check(ctx, version)
	if err != nil {
		sb.writeLine("  %s[UPDATE]%s Could not reach GitHub: %v", cYellow, cReset, err)
		return
	}

	// An on-demand check still advances the schedule, so it replaces the next
	// background check rather than running alongside it.
	rec := ledger.Load()
	rec.LastCheck = time.Now()
	rec.LastSuccess = rec.LastCheck

	if !result.Available {
		if !update.IsRelease(version) {
			sb.writeLine("  This is a development build (%s), so there is nothing to compare against.", version)
		} else {
			sb.writeLine("  %s[UPDATE]%s You are on the newest %s release (%s).", cGreen, cReset, channel, version)
		}
		_ = ledger.Save(rec)
		return
	}

	rec.LastSeenLatest = result.Latest
	_ = ledger.Save(rec)
	announceUpdate(sb, hub, result, method)
}

// reportInterruptedMonitoring tells the user when the previous run ended while
// the machine was armed.
//
// That is the case worth shouting about: LeaveSafe was watching, then it
// stopped — a crash, a flat battery, a reboot, or someone closing the window
// precisely because it was watching them — and nothing has been watched since.
// Coming back up quietly disarmed would present that gap as a normal start.
//
// Re-arming automatically is opt-in, because the first thing a freshly booted
// laptop does is open its lid and accept keystrokes from whoever turned it on.
func reportInterruptedMonitoring(sb *statusBar, hub *ws.Hub, store *state.Store, prev state.State, loadErr error, restore bool) {
	if loadErr != nil {
		log.Warnf("Could not read the last recorded state: %v", loadErr)
		return
	}
	if !prev.Armed {
		return
	}

	since := "an unknown time"
	if !prev.ChangedAt.IsZero() {
		since = prev.ChangedAt.Format("2006-01-02 15:04:05")
	}
	sb.writeLine("  %s%s[!] LeaveSafe was ARMED when it last stopped (armed at %s).%s",
		cRed, cBold, since, cReset)
	sb.writeLine("  %s    This machine has not been monitored since then.%s", cRed, cReset)

	if !restore {
		sb.writeLine("  %s    Starting disarmed. Set \"restore_armed_state\": true in the config to re-arm automatically.%s",
			cDim, cReset)
		if el := hub.EventLogger(); el != nil {
			el.Log(eventlog.Event{
				Type:    eventlog.EventDisarm,
				Message: "Previous run ended while armed; started disarmed",
			})
		}
		// Clear the stale record so the warning fires once for the run it
		// describes rather than on every start from here on.
		if err := store.Save(false); err != nil {
			log.Warnf("Could not clear the recorded armed state: %v", err)
		}
		return
	}

	sb.writeLine("  %s    Re-arming, as restore_armed_state is enabled.%s", cYellow, cReset)
	hub.RestoreArmed(prev.ChangedAt)
}

func buildDashboard(out *os.File, srv *server.Server, authMgr *auth.Manager,
	hub *ws.Hub, sensorMgr *monitor.Manager, remoteState remote.State) *statusBar {
	certFP := remoteState.CertFP
	termW, termH, err := term.GetSize(int(out.Fd()))
	if err != nil || termW < 80 || termH < 20 {
		termW, termH = 120, 40
	}

	fmt.Fprintf(out, "\033[2J\033[H")

	row := 1

	banner := []string{
		"  ██╗     ███████╗ █████╗ ██╗   ██╗███████╗███████╗ █████╗ ███████╗███████╗",
		"  ██║     ██╔════╝██╔══██╗██║   ██║██╔════╝██╔════╝██╔══██╗██╔════╝██╔════╝",
		"  ██║     █████╗  ███████║██║   ██║█████╗  ███████╗███████║█████╗  █████╗  ",
		"  ██║     ██╔══╝  ██╔══██║╚██╗ ██╔╝██╔══╝  ╚════██║██╔══██║██╔══╝  ██╔══╝  ",
		"  ███████╗███████╗██║  ██║ ╚████╔╝ ███████╗███████║██║  ██║██║     ███████╗",
		"  ╚══════╝╚══════╝╚═╝  ╚═╝  ╚═══╝  ╚══════╝╚══════╝╚═╝  ╚═╝╚═╝     ╚══════╝",
	}
	for _, line := range banner {
		fmt.Fprintf(out, "%s%s%s\n", cCyan, line, cReset)
		row++
	}
	fmt.Fprintf(out, "  %s%s%s  %sDevice Security Monitor%s\n", cBold, version, cReset, cDim, cReset)
	row++

	sep := "  " + strings.Repeat("─", termW-4)
	fmt.Fprintf(out, "%s%s%s\n", cDim, sep, cReset)
	row++

	fmt.Fprintf(out, "\n")
	row++

	urls := reachableURLs(srv, remoteState)

	// A QR code is rendered for every reachable URL, not just the first. With
	// remote access on, the public URL only works from outside the network:
	// scanning it from a phone on the same Wi-Fi needs NAT hairpinning, which
	// plenty of routers do not do. `qr <n>` switches to the local URL instead.
	qrCodes := make([][]string, 0, len(urls))
	for _, u := range urls {
		lines, err := qr.Lines(pairingURL(u, authMgr.RawPairingKey(), certFP))
		if err != nil {
			log.Warnf("Could not render QR code for %s: %v", u, err)
			lines = nil
		}
		qrCodes = append(qrCodes, lines)
	}

	// Size the box to the largest code so switching between them never
	// overflows into the rest of the layout.
	const qrIndent = 2
	qrW, qrH := 0, 0
	for _, lines := range qrCodes {
		if len(lines) > qrH {
			qrH = len(lines)
		}
		if len(lines) > 0 {
			if w := utf8.RuneCountInString(lines[0]); w > qrW {
				qrW = w
			}
		}
	}

	const gap = 3
	statusCol := qrIndent + qrW + gap + 1
	statusW := termW - statusCol - 1
	if statusW > 50 {
		statusW = 50
	}
	if statusW < 30 {
		statusW = 30
	}

	fmt.Fprintf(out, "  %sScan to connect:%s\n", cDim, cReset)
	row++

	qrStartRow := row

	sb := &statusBar{
		out:          out,
		hub:          hub,
		sensorMgr:    sensorMgr,
		gridRow:      qrStartRow,
		gridCol:      statusCol,
		gridWidth:    statusW,
		qrCol:        qrIndent + 1,
		qrBoxW:       qrW,
		qrBoxH:       qrH,
		qrCodes:      qrCodes,
		key:          authMgr.PairingKey(),
		rawKey:       authMgr.RawPairingKey(),
		urls:         urls,
		remoteStatus: remoteStatusLine(remoteState),
		certFP:       certFP,
	}

	statusLines := sb.gridLines()
	statusH := len(statusLines)

	totalRows := qrH
	if statusH > totalRows {
		totalRows = statusH
	}

	qrVOff, statusVOff := 0, 0
	if qrH < totalRows {
		qrVOff = (totalRows - qrH) / 2
	}
	if statusH < totalRows {
		statusVOff = (totalRows - statusH) / 2
	}
	sb.gridRow = qrStartRow + statusVOff
	sb.qrRow = qrStartRow + qrVOff

	for i := 0; i < totalRows; i++ {
		si := i - statusVOff
		if si >= 0 && si < len(statusLines) {
			fmt.Fprintf(out, "\033[%d;%dH%s", qrStartRow+i, statusCol, statusLines[si])
		}
	}
	sb.drawQR()

	row = qrStartRow + totalRows

	fmt.Fprintf(out, "\033[%d;1H\n", row)
	row++
	fmt.Fprintf(out, "\033[%d;1H  %sCommands:%s arm, disarm, status, stop, test, trigger <sensor>, history, urls, qr <n>, cert, mode, lang, rotate-key, help  %s│%s  %sCtrl+C to quit%s\n",
		row, cDim, cReset, cDim, cReset, cDim, cReset)
	row++
	fmt.Fprintf(out, "\033[%d;1H%s%s%s\n", row, cDim, sep, cReset)
	row++

	headerRows := row - 1
	if headerRows > termH-3 {
		headerRows = termH - 3
	}
	fmt.Fprintf(out, "\033[%d;%dr", headerRows+1, termH)
	fmt.Fprintf(out, "\033[%d;1H", headerRows+1)

	return sb
}

// connectionModeName names a connection mode for a log line.
func connectionModeName(remote bool) string {
	if remote {
		return "remote access"
	}
	return "local network only"
}

// promptRemoteAccess asks the connection-mode question and saves the answer.
//
// This runs on every interactive start rather than only the first, because the
// first-run-only version was answered by accident: the phone's settings screen
// sends remote_access on every save, so saving any unrelated setting turned the
// unset value into a definite false and the question never came back.
func promptRemoteAccess(cfg *config.Config, lang language) {
	current := cfg.RemoteAccess != nil && *cfg.RemoteAccess
	remote := askConnectionMode(os.Stdin, os.Stdout, lang, current)
	cfg.RemoteAccess = &remote

	if err := config.Save(cfg); err != nil {
		log.Warnf("Failed to save config: %v", err)
	}

	// "Trying" rather than "enabled", because at this point it is a request:
	// the listener, the router and the ISP all still get a say, and the
	// dashboard reports what they said. Announcing success here is how a user
	// ends up scanning a QR code for an address nothing will answer.
	if remote {
		promptResult(os.Stdout, lang.remoteAsked)
	} else {
		promptResult(os.Stdout, lang.wifiChosen)
	}
	fmt.Fprintln(os.Stdout)
}

// parseModeChoice reads a connection-mode answer. ok is false when the user
// pressed enter, typed something else, or the input ended — all of which mean
// "leave it alone" rather than a mode.
func parseModeChoice(typed string) (want, ok bool) {
	switch strings.TrimSpace(typed) {
	case "1":
		return false, true
	case "2":
		return true, true
	default:
		return false, false
	}
}

// consoleDeps is everything the interactive command loop acts on. It is a
// struct rather than nine parameters so that a test can build only the two or
// three a given command actually touches, and leave the rest zero.
type consoleDeps struct {
	hub           *ws.Hub
	sb            *statusBar
	localAlarm    *alarm.Alarm
	authMgr       *auth.Manager
	installMethod update.Method
	updateLedger  *update.Ledger
	srv           *server.Server
	remoteCtl     *remote.Controller
	cfg           *config.Config
}

// runConsole reads typed commands until the reader is exhausted. The reader is
// a parameter rather than os.Stdin directly so the loop can be driven from a
// test; the running program always passes os.Stdin.
func runConsole(ctx context.Context, in io.Reader, d consoleDeps) {
	hub, sb, localAlarm := d.hub, d.sb, d.localAlarm
	authMgr, updateLedger := d.authMgr, d.updateLedger
	srv, remoteCtl, cfg := d.srv, d.remoteCtl, d.cfg
	installMethod := d.installMethod

	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "test":
			hub.PushAlert(ws.NewAlert("test", "warning", "Test alert from console"))
			sb.writeLine("  %s[TEST]%s Alert sent to %d client(s)", cYellow, cReset, hub.ClientCount())
		case strings.HasPrefix(line, "trigger "):
			name := strings.TrimSpace(line[8:])
			if !hub.TriggerSensorTest(name) {
				sb.writeLine("  Unknown sensor: %q  (type 'help')", name)
			} else {
				sb.writeLine("  %s[TEST]%s Sensor %q triggered", cYellow, cReset, name)
			}
		case line == "stop" || line == "silence":
			if localAlarm.IsPlaying() || hub.ClientCount() > 0 {
				// Through the hub rather than the alarm directly, so every
				// paired phone stops sounding too.
				hub.DismissAlarm()
				sb.writeLine("  %s[ALARM]%s Alarm dismissed from console", cYellow, cReset)
			} else {
				sb.writeLine("  No alarm is currently active")
			}

		case line == "arm":
			if hub.IsArmed() {
				sb.writeLine("  Already armed since %s", clockTime(hub.ArmedAt()))
				break
			}
			hub.Arm()
			sb.writeLine("  %s[ARM]%s Armed from console", cGreen, cReset)

		case line == "disarm":
			if !hub.IsArmed() {
				sb.writeLine("  Already disarmed")
				break
			}
			pin := ""
			if hub.PinRequired() {
				sb.writeLine("  PIN required to disarm. Type it and press enter:")
				if !scanner.Scan() {
					break
				}
				pin = strings.TrimSpace(scanner.Text())
			}
			if err := hub.DisarmWithPin("console", pin); err != nil {
				sb.writeLine("  %s[PIN]%s Refused: %v", cRed, cReset, err)
				break
			}
			sb.writeLine("  %s[DISARM]%s Disarmed from console", cGreen, cReset)

		case line == "status":
			if hub.IsArmed() {
				sb.writeLine("  %sARMED%s since %s (%s ago)", cGreen, cReset,
					clockTime(hub.ArmedAt()),
					time.Since(hub.ArmedAt()).Round(time.Second))
			} else {
				sb.writeLine("  %sDISARMED%s", cDim, cReset)
			}
			sb.writeLine("  Phones connected: %d", hub.ClientCount())
			for _, s := range hub.GetSensorInfos() {
				mark := "off"
				switch {
				case !s.Available:
					mark = "unavailable"
				case s.Failure != "":
					// Named rather than folded into "on": this is the state the
					// user most needs to see and least expects to be in.
					mark = "FAILED — " + s.Failure
				case s.Enabled:
					mark = "on"
				}
				sb.writeLine("    %-10s %s", s.Name, mark)
			}
		case line == "history":
			evts, err := eventlog.ReadLast(filepath.Join(config.ConfigDir(), eventLogFileName), 20)
			if err != nil {
				sb.writeLine("  No event history available")
			} else if len(evts) == 0 {
				sb.writeLine("  No events recorded yet")
			} else {
				for _, ev := range evts {
					ts := clockTime(ev.Timestamp)
					if ev.Sensor != "" {
						sb.writeLine("  %s [%s] %s — %s", ts, ev.Type, ev.Sensor, ev.Message)
					} else {
						sb.writeLine("  %s [%s] %s", ts, ev.Type, ev.Message)
					}
				}
			}
		case line == "rotate-key":
			newKey, err := authMgr.Regenerate()
			if err != nil {
				sb.writeLine("  %s[ERROR]%s Failed to rotate key: %v", cRed, cReset, err)
			} else {
				sb.key = newKey
				sb.refresh()
				sb.rekeyQR(authMgr.RawPairingKey())
				// Without this the stored key still holds the one just
				// invalidated, and the next start would come back with it.
				if sb.keyPath != "" {
					if err := auth.SaveKeyFile(sb.keyPath, authMgr.RawPairingKey()); err != nil {
						sb.writeLine("  %s[KEY]%s Rotated, but the stored key could not be updated: %v", cRed, cReset, err)
					}
				}
				sb.writeLine("  %s[KEY]%s Pairing key rotated. New key: %s%s%s", cGreen, cReset, cBold, newKey, cReset)
				sb.writeLine("  %s[KEY]%s All existing sessions invalidated. The QR code now encodes the new key.", cYellow, cReset)
			}
		case line == "urls":
			for i, u := range sb.urls {
				marker := " "
				if i == sb.qrURLIdx {
					marker = "*"
				}
				sb.writeLine("  %s[%d]%s %s%s", cBold, i+1, marker, u, cReset)
			}
			sb.writeLine("  %sUse 'qr <n>' to show the QR code for one of these.%s", cDim, cReset)

		case strings.HasPrefix(line, "qr "):
			n, err := strconv.Atoi(strings.TrimSpace(line[3:]))
			if err != nil {
				sb.writeLine("  Usage: qr <n>   (type 'urls' to list them)")
				break
			}
			if shown := sb.showQR(n); shown == "" {
				sb.writeLine("  No URL %d. Type 'urls' to list them.", n)
			} else {
				sb.writeLine("  %s[QR]%s Now showing %s", cGreen, cReset, shown)
			}

		case line == "cert":
			if sb.certFP == "" {
				sb.writeLine("  No TLS certificate — this server is running over plain HTTP on the local network.")
			} else {
				sb.writeLine("  %sTLS certificate SHA-256 fingerprint:%s", cBold, cReset)
				sb.writeLine("  %s", sb.certFP)
				sb.writeLine("  %sYour phone will warn that this certificate is untrusted; it is self-signed.%s", cDim, cReset)
				sb.writeLine("  %sCompare the fingerprint above with the one the warning shows before accepting.%s", cDim, cReset)
			}

		case line == "update":
			checkForUpdateNow(sb, hub, installMethod, updateLedger)

		case line == "mode":
			st := remoteCtl.State()
			cur := 1
			if st.Enabled {
				cur = 2
			}
			sb.writeLine("  [1] Wi-Fi only   [2] Remote access   (currently %d)", cur)
			sb.writeLine("  Type 1 or 2, or press enter to leave it alone:")
			if !scanner.Scan() {
				break
			}
			want, ok := parseModeChoice(scanner.Text())
			if !ok {
				sb.writeLine("  Left unchanged")
				break
			}
			if want == st.Enabled {
				sb.writeLine("  Already in that mode")
				break
			}
			// Written to disk before the listener moves, so a crash in between
			// leaves the config saying what the user asked for rather than what
			// the process happened to be doing.
			cfg.RemoteAccess = &want
			if err := config.Save(cfg); err != nil {
				sb.writeLine("  %s[NET]%s Could not save the setting: %v", cRed, cReset, err)
				break
			}
			next := remoteCtl.Disable()
			if want {
				next = remoteCtl.Enable(ctx)
			}
			applyRemoteState(sb, hub, srv, next)
			sb.writeLine("  %s[NET]%s Connection mode: %s", cGreen, cReset, connectionModeName(want))

		case line == "lang":
			sb.writeLine("  [1] Türkçe   [2] English   (currently %s)", languageByCode(cfg.Language).code)
			sb.writeLine("  Type 1 or 2, or press enter to leave it alone:")
			if !scanner.Scan() {
				break
			}
			code, ok := parseLanguageChoice(scanner.Text())
			if !ok {
				sb.writeLine("  Left unchanged")
				break
			}
			cfg.Language = code
			if err := config.Save(cfg); err != nil {
				sb.writeLine("  %s[LANG]%s Could not save the setting: %v", cRed, cReset, err)
				break
			}
			// Said plainly rather than implied. The language reaches the two
			// questions asked before this dashboard exists and nothing on it, so
			// a user who changes it here and watches for the screen to turn
			// would be waiting for something that is never going to happen.
			sb.writeLine("  %s[LANG]%s Startup questions will be in %s from the next start.",
				cGreen, cReset, code)

		case line == "help":
			sb.writeLine("  Commands: arm, disarm, status, stop, test, trigger <sensor>, history, urls, qr <n>, cert, mode, lang, update, rotate-key, help")
		case line == "":
		default:
			sb.writeLine("  Unknown command: %q  (type 'help')", line)
		}
	}
}

func runStatusTicker(ctx context.Context, sb *statusBar) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sb.refresh()
		}
	}
}

// buildLocationTracker assembles the position sources the config asks for.
// Returns nil when the feature is off, which is the default — with it off no
// scan runs and no request leaves the machine.
func buildLocationTracker(cfg *config.Config) *location.Tracker {
	lc := cfg.Location
	if !lc.Enabled {
		return nil
	}

	var providers []location.Provider

	if lc.WiFiEnabled {
		p := location.NewWiFiProvider(lc.GeolocateURL, lc.GeolocateKey)
		providers = append(providers, p)
		if !p.Available() {
			log.Warn("Wi-Fi positioning is enabled but unavailable on this machine — falling back to the other sources")
		}
	}
	if lc.IPFallback {
		providers = append(providers, location.NewIPProvider(lc.IPLookupURL))
	}

	tracker := location.NewTracker(providers, time.Duration(lc.PollSeconds)*time.Second)

	switch {
	case tracker.HasProviders():
		log.Info("Location tracking enabled — position is reported while armed")
	case lc.PhoneAnchor:
		// No live source, but the phone's own position at arm time is still a
		// real answer to "where did I leave it", so the tracker stays.
		log.Info("Location tracking enabled with no live source — using the phone's position at arm time only")
	default:
		log.Warn("Location tracking is enabled but every source is switched off")
	}

	return tracker
}

func registerSensors(mgr *monitor.Manager, cfg *config.Config) {
	mgr.Register(monitor.NewPowerSensor())
	mgr.Register(monitor.NewLidSensor())
	mgr.Register(monitor.NewUSBSensor())
	mgr.Register(monitor.NewScreenSensor())
	mgr.Register(monitor.NewNetworkSensor())
	mgr.Register(monitor.NewInputSensorWithThreshold(cfg.InputThreshold))

	// Apply saved sensor preferences from config
	if cfg.EnabledSensors != nil {
		for name, enabled := range cfg.EnabledSensors {
			if enabled {
				mgr.Enable(name)
			}
		}
	}
}

// logSensorAvailability writes down which sensors can work on this machine.
//
// It is deliberately not part of registering them. Asking a sensor whether it
// is available can be expensive — on Windows the lid's answer comes from WMI,
// which on a machine with no battery has been seen to take half a minute — and
// nothing about the server coming up should wait behind a log line. Run this on
// its own goroutine and the phone can reach the laptop while the answer is
// still being worked out.
func logSensorAvailability(mgr *monitor.Manager) {
	// Ask them all at once and wait for the answers, so the six probes overlap
	// instead of queueing. Everything below then reads a settled value, and so
	// does anything that pairs while this is still running: it is told what is
	// known so far rather than made to wait for the rest.
	mgr.PrimeAvailability()

	sensors := mgr.Sensors()
	available := 0
	for _, s := range sensors {
		if s.Available() {
			available++
			log.WithFields(log.Fields{"sensor": s.Name(), "display": s.DisplayName(), "enabled": mgr.IsEnabled(s.Name())}).Info("Sensor registered")
		} else {
			log.WithFields(log.Fields{"sensor": s.Name(), "display": s.DisplayName()}).Info("Sensor unavailable")
		}
	}
	log.Infof("%d/%d sensors available", available, len(sensors))
}
