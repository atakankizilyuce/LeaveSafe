package main

import (
	"bufio"
	"context"
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

	key          string
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
func newHeadlessStatusBar(hub *ws.Hub, sensorMgr *monitor.Manager, key string, urls []string, certFP, keyPath string) *statusBar {
	return &statusBar{
		out:       io.Discard,
		hub:       hub,
		sensorMgr: sensorMgr,
		key:       key,
		urls:      urls,
		certFP:    certFP,
		headless:  true,
		keyPath:   keyPath,
		qrURLIdx:  -1,
	}
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
		if sb.sensorMgr.IsEnabled(s.Name()) && s.Available() {
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

// stripANSI removes the colour and cursor escapes the dashboard uses, so the
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

	log.SetFormatter(&log.TextFormatter{
		TimestampFormat: "15:04:05",
		FullTimestamp:   true,
	})

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

	// First-run: ask connection mode if not yet configured. There is nobody at
	// a headless start to answer, and blocking on stdin there would hang the
	// service forever, so it takes the safe answer: local network only.
	if cfg.RemoteAccess == nil {
		if *headless {
			local := false
			cfg.RemoteAccess = &local
			if err := config.Save(cfg); err != nil {
				log.Warnf("Failed to save config: %v", err)
			}
			log.Info("First headless start: remote access left off. Run LeaveSafe in a terminal to change it.")
		} else {
			promptRemoteAccess(cfg)
		}
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

	// Remote access: set up TLS, UPnP, and public IP
	var portMapping *network.PortMapping
	var publicIP string
	srvCfg := server.Config{Hub: hub, Port: port, DevMode: *devMode}

	var certFP string
	if remoteEnabled {
		if port == 0 {
			port = cfg.RemotePort
			srvCfg.Port = port
		}

		cert, fp, err := server.GenerateOrLoadCert(config.ConfigDir())
		if err != nil {
			// Remote access publishes this port to the internet. Serving it
			// over plain HTTP would put the pairing key — the only thing
			// guarding the alarm — in cleartext on the wire. Staying on the LAN
			// is the safe failure, so remote access does not come up at all.
			log.Errorf("TLS certificate error: %v", err)
			log.Error("Remote access DISABLED: refusing to expose this port without TLS.")
			log.Error("Pairing is still available on the local network. Fix the certificate to restore remote access.")
			remoteEnabled = false
		} else {
			srvCfg.TLSCert = &cert
			srvCfg.CertFP = fp
			certFP = fp
			// Announced to every connecting client, so a phone that arrived by
			// QR code can check it reached the server the code was printed for
			// before it offers the pairing key.
			hub.SetCertFingerprint(fp)
		}
	}

	srv := server.New(srvCfg)
	if err := srv.Listen(); err != nil {
		log.Fatalf("Failed to bind port: %v", err)
	}

	if remoteEnabled {
		pm, err := network.OpenPort(srv.Port())
		if err != nil {
			log.Warnf("UPnP failed: %v — manual port forwarding required (port %d)", err, srv.Port())
		} else {
			portMapping = pm
			if ip, err := pm.ExternalIP(); err == nil {
				publicIP = ip
			}
		}
		if publicIP == "" {
			if ip, err := network.GetPublicIP(); err == nil {
				publicIP = ip
			} else {
				log.Warnf("Could not determine public IP: %v", err)
			}
		}
	}

	var sb *statusBar
	if *headless {
		sb = newHeadlessStatusBar(hub, sensorMgr, authMgr.PairingKey(),
			reachableURLs(srv, publicIP), certFP, keyPath)
		logHeadlessStartup(sb, srv, certFP)
	} else {
		sb = buildDashboard(os.Stdout, srv, authMgr, hub, sensorMgr, publicIP, certFP)
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

	if cfg.UpdateCheckEnabled() {
		safe.Go("update-check", func() { reportAvailableUpdate(sb) })
	}

	hub.SetAlarmTriggerCallback(func() {
		localAlarm.Start()
	})
	hub.SetAlarmDismissCallback(func() {
		localAlarm.Stop()
	})
	hub.SetDisconnectCallback(func() {
		log.Warn("All clients disconnected while armed — local alarm triggered")
		localAlarm.Start()
	})
	hub.SetClientChangeCallback(func(_ int, _ bool) {
		sb.refresh()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub.SetLocationTracker(ctx, buildLocationTracker(cfg))

	// Every long-lived loop is supervised. A panic in any one of them used to
	// take the whole process down, which for an armed machine means the user
	// walks back to a laptop that stopped watching itself and never said so.
	safe.Supervise(ctx, "alert-dispatcher", hub.RunAlertDispatcher)
	safe.Supervise(ctx, "heartbeat", hub.RunHeartbeat)
	if !*headless {
		safe.Supervise(ctx, "status-ticker", func(c context.Context) { runStatusTicker(c, sb) })
		// No terminal means no stdin to read commands from.
		safe.Supervise(ctx, "console", func(context.Context) { runConsole(hub, sb, localAlarm, authMgr) })
	}

	if portMapping != nil {
		safe.Supervise(ctx, "upnp-keepalive", portMapping.KeepAlive)
	}

	connMode := cfg.ConnectionMode
	if connMode == "bluetooth" || connMode == "both" {
		bleServer := ble.NewServer(hub)
		safe.Supervise(ctx, "ble-server", func(c context.Context) {
			if err := bleServer.Start(c); err != nil {
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
		if portMapping != nil {
			_ = portMapping.Close()
		}
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
// The fingerprint rides along so the phone can check which server it reached
// before it offers the key, and so the value is on the phone's own screen to
// compare against the browser's certificate warning. Previously it was only on
// the laptop, which is the one place nobody looks while holding a phone. The
// colons are dropped to keep the QR code small; both ends compare on the hex.
//
// This does not turn a self-signed certificate into a verified one. See
// SECURITY.md for what it does and does not catch.
func pairingURL(base, rawKey, certFP string) string {
	url := base + "?key=" + rawKey
	if certFP != "" {
		url += "&fp=" + strings.ToLower(strings.ReplaceAll(certFP, ":", ""))
	}
	return url
}

// reachableURLs returns every address a phone could connect to, public one
// first when remote access is up.
func reachableURLs(srv *server.Server, publicIP string) []string {
	urls := srv.URLs()
	if publicIP == "" {
		return urls
	}
	scheme := "http"
	if srv.IsTLS() {
		scheme = "https"
	}
	return append([]string{fmt.Sprintf("%s://%s:%d", scheme, publicIP, srv.Port())}, urls...)
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

// reportAvailableUpdate says so if a newer release exists.
//
// Nothing is downloaded and nothing is replaced: a security program that
// silently rewrites itself from the network is a larger trust decision than the
// user made when they downloaded a single file. But a single file is also a
// file nothing updates, and the whole point of hearing about a fix is hearing
// about it before you need it. A failed check is not worth a warning — GitHub
// being unreachable says nothing about this machine — so it goes to the log at
// debug level and no further.
func reportAvailableUpdate(sb *statusBar) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := update.Checker{}.Check(ctx, version)
	if err != nil {
		log.Debugf("Update check failed: %v", err)
		return
	}
	if !result.Available {
		return
	}

	sb.writeLine("  %s[UPDATE]%s %s is available — you are running %s", cCyan, cReset, result.Latest, version)
	url := result.URL
	if url == "" {
		url = repoURL + "/releases/latest"
	}
	sb.writeLine("  %s         %s%s", cDim, url, cReset)
	sb.writeLine("  %s         Set \"update_check\": false in the config to stop checking.%s", cDim, cReset)
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
	hub.RestoreArmed()
}

func buildDashboard(out *os.File, srv *server.Server, authMgr *auth.Manager,
	hub *ws.Hub, sensorMgr *monitor.Manager, publicIP, certFP string) *statusBar {
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

	urls := reachableURLs(srv, publicIP)

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

	remoteStatus := ""
	if publicIP != "" {
		remoteStatus = fmt.Sprintf("ACTIVE — %s:%d", publicIP, srv.Port())
	}

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
		urls:         urls,
		remoteStatus: remoteStatus,
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
	fmt.Fprintf(out, "\033[%d;1H  %sCommands:%s test, trigger <sensor>, stop, history, urls, qr <n>, cert, rotate-key, help  %s│%s  %sCtrl+C to quit%s\n",
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

func promptRemoteAccess(cfg *config.Config) {
	fmt.Printf("\n  %s%sBağlantı modu seçin / Select connection mode:%s\n\n", cBold, cCyan, cReset)
	fmt.Printf("  %s[1]%s WiFi (aynı ağ / same network)\n", cBold, cReset)
	fmt.Printf("      %sYalnızca aynı WiFi ağından bağlantı%s\n\n", cDim, cReset)
	fmt.Printf("  %s[2]%s Uzaktan Erişim / Remote Access\n", cBold, cReset)
	fmt.Printf("      %sMobil veri veya farklı ağdan bağlantı (UPnP gerekir)%s\n\n", cDim, cReset)
	fmt.Printf("  Seçiminiz / Your choice (1/2): ")

	scanner := bufio.NewScanner(os.Stdin)
	remote := false
	if scanner.Scan() {
		choice := strings.TrimSpace(scanner.Text())
		if choice == "2" {
			remote = true
		}
	}
	cfg.RemoteAccess = &remote

	if err := config.Save(cfg); err != nil {
		log.Warnf("Failed to save config: %v", err)
	}

	if remote {
		fmt.Printf("\n  %s✓ Uzaktan erişim aktif — Remote access enabled%s\n\n", cGreen, cReset)
	} else {
		fmt.Printf("\n  %s✓ WiFi modu seçildi — WiFi mode selected%s\n\n", cGreen, cReset)
	}
}

func runConsole(hub *ws.Hub, sb *statusBar, localAlarm *alarm.Alarm, authMgr *auth.Manager) {
	scanner := bufio.NewScanner(os.Stdin)
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
			if localAlarm.IsPlaying() {
				localAlarm.Stop()
				sb.writeLine("  %s[ALARM]%s Alarm dismissed from console", cYellow, cReset)
			} else {
				sb.writeLine("  No alarm is currently active")
			}
		case line == "history":
			evts, err := eventlog.ReadLast(filepath.Join(config.ConfigDir(), eventLogFileName), 20)
			if err != nil {
				sb.writeLine("  No event history available")
			} else if len(evts) == 0 {
				sb.writeLine("  No events recorded yet")
			} else {
				for _, ev := range evts {
					ts := ev.Timestamp.Format("15:04:05")
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

		case line == "help":
			sb.writeLine("  Commands: test, trigger <sensor>, stop, history, urls, qr <n>, cert, rotate-key, help")
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
