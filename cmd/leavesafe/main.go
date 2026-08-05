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
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/eventlog"
	"github.com/leavesafe/leavesafe/internal/location"
	"github.com/leavesafe/leavesafe/internal/monitor"
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

// statusBar owns the dashboard: the QR box, the status grid and the log lines
// that scroll beneath them.
//
// mu guards every field below it. That is not decoration — four goroutines
// reach in here. The console reads and writes it as the user types, the status
// ticker repaints every five seconds, the reachability probe replaces the URL
// list a minute after startup, and the panic handler writes a line from
// wherever it happened. Reading a field directly rather than through one of the
// accessors below is a data race, and the ones that were left were on the
// pairing key and the address list — precisely the two things a torn read would
// print on screen for the user to scan.
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

	codes := renderQRCodes(urls, rawKey, certFP)

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

// urlListWithSelection is urlList plus which of them the QR box is currently
// showing, taken together so the two cannot come from either side of a change.
func (sb *statusBar) urlListWithSelection() ([]string, int) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return append([]string(nil), sb.urls...), sb.qrURLIdx
}

// setKey records a freshly rotated pairing key for the dashboard to draw.
func (sb *statusBar) setKey(key string) {
	sb.mu.Lock()
	sb.key = key
	sb.mu.Unlock()
	sb.refresh()
}

// certFingerprint returns the fingerprint the QR codes and the header carry.
func (sb *statusBar) certFingerprint() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.certFP
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

	cfg := loadConfig()
	chooseConnectionMode(cfg, *headless)

	a, err := startApp(appOptions{
		cfg:      cfg,
		headless: *headless,
		devMode:  *devMode,
		out:      os.Stdout,
		in:       os.Stdin,
	})
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer a.close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	safe.Go("shutdown", func() {
		<-sigCh
		a.shutdown()
		os.Exit(0)
	})

	if err := a.serve(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// loadConfig reads the config and reports what it had to correct.
//
// A hand-edited config is the normal way to change most of these settings, and a
// typo should not produce a monitor that never heartbeats.
func loadConfig() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		log.Warnf("Failed to load config, using defaults: %v", err)
	}
	for _, note := range cfg.Validate() {
		log.Warnf("Config: %s", note)
	}
	return cfg
}

// chooseConnectionMode settles whether this run offers remote access.
//
// It is asked on every interactive start, with the saved value as the default,
// so a user who changed it from their phone sees what is in force and can change
// it back without hunting for the setting.
//
// A headless start has nobody to answer and blocking on stdin there would hang
// the service forever, so it takes what is stored. Every autostart entry this
// program writes passes -headless, so no unattended start can reach the prompt.
func chooseConnectionMode(cfg *config.Config, headless bool) {
	if !headless {
		// Language first, and only once. It decides how the next question is
		// worded, so there is no order in which it could come second.
		promptRemoteAccess(cfg, ensureLanguage(cfg))
		return
	}

	if cfg.RemoteAccess == nil {
		local := false
		cfg.RemoteAccess = &local
		if err := config.Save(cfg); err != nil {
			log.Warnf("Failed to save config: %v", err)
		}
	}
	log.Infof("Connection mode: %s (from config, no terminal to ask)",
		connectionModeName(*cfg.RemoteAccess))
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
	for _, u := range sb.urlList() {
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

// qrIndent is how far from the left edge the QR box starts, and gap is the space
// between it and the status grid beside it.
const (
	qrIndent = 2
	gap      = 3
)

func buildDashboard(out *os.File, srv *server.Server, authMgr *auth.Manager,
	hub *ws.Hub, sensorMgr *monitor.Manager, remoteState remote.State,
) *statusBar {
	termW, termH := terminalSize(out)
	sep := "  " + strings.Repeat("─", termW-4)

	fmt.Fprintf(out, "\033[2J\033[H")
	row := drawHeader(out, sep)

	urls := reachableURLs(srv, remoteState)
	qrCodes := renderQRCodes(urls, authMgr.RawPairingKey(), remoteState.CertFP)
	qrW, qrH := qrBoxSize(qrCodes)
	statusCol, statusW := statusColumn(termW, qrW)

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
		certFP:       remoteState.CertFP,
	}

	row = qrStartRow + drawCodeAndStatus(out, sb, qrStartRow, statusCol)
	row = drawFooter(out, sep, row)

	// Everything above is drawn once and must stay put, so the scrolling region
	// starts below it. Without this the log would scroll over the QR code the
	// user is trying to scan.
	headerRows := min(row-1, termH-3)
	fmt.Fprintf(out, "\033[%d;%dr", headerRows+1, termH)
	fmt.Fprintf(out, "\033[%d;1H", headerRows+1)

	return sb
}

// terminalSize is the size of the terminal, or a shape the dashboard fits in
// when it cannot be asked.
//
// A window too small is treated the same as no answer at all. The layout below
// assumes it has room, and drawing it into eighty by twenty produces a QR code
// cut in half — which is worse than a layout that runs off the edge of a window
// the user can resize.
func terminalSize(out *os.File) (width, height int) {
	w, h, err := term.GetSize(int(out.Fd()))
	if err != nil || w < 80 || h < 20 {
		return 120, 40
	}
	return w, h
}

// drawHeader writes the banner, the version and the rule under them, and returns
// the row the next thing goes on.
func drawHeader(out io.Writer, sep string) int {
	banner := []string{
		"  ██╗     ███████╗ █████╗ ██╗   ██╗███████╗███████╗ █████╗ ███████╗███████╗",
		"  ██║     ██╔════╝██╔══██╗██║   ██║██╔════╝██╔════╝██╔══██╗██╔════╝██╔════╝",
		"  ██║     █████╗  ███████║██║   ██║█████╗  ███████╗███████║█████╗  █████╗  ",
		"  ██║     ██╔══╝  ██╔══██║╚██╗ ██╔╝██╔══╝  ╚════██║██╔══██║██╔══╝  ██╔══╝  ",
		"  ███████╗███████╗██║  ██║ ╚████╔╝ ███████╗███████║██║  ██║██║     ███████╗",
		"  ╚══════╝╚══════╝╚═╝  ╚═╝  ╚═══╝  ╚══════╝╚══════╝╚═╝  ╚═╝╚═╝     ╚══════╝",
	}

	row := 1
	for _, line := range banner {
		fmt.Fprintf(out, "%s%s%s\n", cCyan, line, cReset)
		row++
	}
	fmt.Fprintf(out, "  %s%s%s  %sDevice Security Monitor%s\n", cBold, version, cReset, cDim, cReset)
	row++
	fmt.Fprintf(out, "%s%s%s\n", cDim, sep, cReset)
	row++
	fmt.Fprintf(out, "\n")
	return row + 1
}

// renderQRCodes builds a QR code for every address the laptop can be reached at,
// not just the first.
//
// With remote access on, the public URL only works from outside the network:
// scanning it from a phone on the same Wi-Fi needs NAT hairpinning, which plenty
// of routers do not do. `qr <n>` switches to the local URL instead.
//
// A code that will not render becomes a nil entry rather than a missing one, so
// the indexes stay lined up with the address list they came from.
func renderQRCodes(urls []string, rawKey, certFP string) [][]string {
	codes := make([][]string, 0, len(urls))
	for _, u := range urls {
		lines, err := qr.Lines(pairingURL(u, rawKey, certFP))
		if err != nil {
			log.Warnf("Could not render QR code for %s: %v", u, err)
			lines = nil
		}
		codes = append(codes, lines)
	}
	return codes
}

// qrBoxSize is the size of the largest code, which is the size the box has to
// be: switching between them with `qr <n>` must never overflow the layout.
func qrBoxSize(codes [][]string) (width, height int) {
	for _, lines := range codes {
		height = max(height, len(lines))
		if len(lines) > 0 {
			width = max(width, utf8.RuneCountInString(lines[0]))
		}
	}
	return width, height
}

// statusColumn places the status grid to the right of the QR box and gives it a
// width it can be read at: wide enough for the longest line it holds, narrow
// enough not to sprawl across a maximized window.
func statusColumn(termW, qrW int) (col, width int) {
	col = qrIndent + qrW + gap + 1
	width = min(max(termW-col-1, 30), 50)
	return col, width
}

// drawCodeAndStatus draws the QR box and the status grid side by side, each
// centered against the taller of the two, and returns how many rows that took.
func drawCodeAndStatus(out io.Writer, sb *statusBar, qrStartRow, statusCol int) int {
	statusLines := sb.gridLines()
	statusH := len(statusLines)
	totalRows := max(sb.qrBoxH, statusH)

	qrVOff, statusVOff := 0, 0
	if sb.qrBoxH < totalRows {
		qrVOff = (totalRows - sb.qrBoxH) / 2
	}
	if statusH < totalRows {
		statusVOff = (totalRows - statusH) / 2
	}
	sb.gridRow = qrStartRow + statusVOff
	sb.qrRow = qrStartRow + qrVOff

	for i := range totalRows {
		si := i - statusVOff
		if si >= 0 && si < len(statusLines) {
			fmt.Fprintf(out, "\033[%d;%dH%s", qrStartRow+i, statusCol, statusLines[si])
		}
	}
	sb.drawQR()

	return totalRows
}

// drawFooter writes the command list and the rule under it, and returns the row
// after them.
func drawFooter(out io.Writer, sep string, row int) int {
	fmt.Fprintf(out, "\033[%d;1H\n", row)
	row++
	fmt.Fprintf(out, "\033[%d;1H  %sCommands:%s arm, disarm, status, stop, test, trigger <sensor>, history, urls, qr <n>, cert, mode, lang, rotate-key, help  %s│%s  %sCtrl+C to quit%s\n",
		row, cDim, cReset, cDim, cReset, cDim, cReset)
	row++
	fmt.Fprintf(out, "\033[%d;1H%s%s%s\n", row, cDim, sep, cReset)
	return row + 1
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

// console is the state one typed command acts on: the dependencies, plus the
// input the three commands that ask a follow-up question read their answer from.
//
// The context is not in here. It belongs to the run of a single command rather
// than to the loop, and a context parked in a struct outlives the call it was
// meant to bound.
type console struct {
	consoleDeps
	in *bufio.Scanner
	// arg is whatever followed the command word, trimmed. Empty for every
	// command that takes none.
	arg string
}

// consoleCommand is one thing the user can type at the dashboard.
type consoleCommand struct {
	// takesArg marks the commands typed as a word, a space and something else.
	// Matching on that space is how they have always been recognized, and it is
	// worth keeping: "qr" on its own is not this command with its number left
	// off, it is a word the console does not know, and answering it with "No URL
	// 0" would be a worse reply than saying so.
	takesArg bool
	run      func(ctx context.Context, c *console)
}

// consoleCommands is every command the loop below will answer to. The help text
// is written out rather than generated from these, because the order a person
// wants to read them in is not the order a map hands them back.
var consoleCommands = map[string]consoleCommand{
	"test":       {run: consoleTest},
	"trigger":    {takesArg: true, run: consoleTrigger},
	"stop":       {run: consoleStop},
	"silence":    {run: consoleStop},
	"arm":        {run: consoleArm},
	"disarm":     {run: consoleDisarm},
	"status":     {run: consoleStatus},
	"history":    {run: consoleHistory},
	"rotate-key": {run: consoleRotateKey},
	"urls":       {run: consoleURLs},
	"qr":         {takesArg: true, run: consoleQR},
	"cert":       {run: consoleCert},
	"update":     {run: consoleUpdate},
	"mode":       {run: consoleMode},
	"lang":       {run: consoleLang},
	"help":       {run: consoleHelp},
}

// runConsole reads typed commands until the reader is exhausted. The reader is
// a parameter rather than os.Stdin directly so the loop can be driven from a
// test; the running program always passes os.Stdin.
func runConsole(ctx context.Context, in io.Reader, d consoleDeps) {
	c := &console{consoleDeps: d, in: bufio.NewScanner(in)}

	for c.in.Scan() {
		line := strings.TrimSpace(c.in.Text())
		if line == "" {
			continue
		}

		name, arg, hasArg := strings.Cut(line, " ")
		cmd, known := consoleCommands[name]
		// A command given an argument it does not take is as unknown as a word
		// that names nothing: "arm now" is not an instruction to arm.
		if !known || cmd.takesArg != hasArg {
			c.sb.writeLine("  Unknown command: %q  (type 'help')", line)
			continue
		}

		c.arg = strings.TrimSpace(arg)
		cmd.run(ctx, c)
	}
}

func consoleTest(_ context.Context, c *console) {
	c.hub.PushAlert(ws.NewAlert("test", "warning", "Test alert from console"))
	c.sb.writeLine("  %s[TEST]%s Alert sent to %d client(s)", cYellow, cReset, c.hub.ClientCount())
}

func consoleTrigger(_ context.Context, c *console) {
	if !c.hub.TriggerSensorTest(c.arg) {
		c.sb.writeLine("  Unknown sensor: %q  (type 'help')", c.arg)
		return
	}
	c.sb.writeLine("  %s[TEST]%s Sensor %q triggered", cYellow, cReset, c.arg)
}

func consoleStop(_ context.Context, c *console) {
	if !c.localAlarm.IsPlaying() && c.hub.ClientCount() == 0 {
		c.sb.writeLine("  No alarm is currently active")
		return
	}
	// Through the hub rather than the alarm directly, so every paired phone
	// stops sounding too.
	c.hub.DismissAlarm()
	c.sb.writeLine("  %s[ALARM]%s Alarm dismissed from console", cYellow, cReset)
}

func consoleArm(_ context.Context, c *console) {
	if c.hub.IsArmed() {
		c.sb.writeLine("  Already armed since %s", clockTime(c.hub.ArmedAt()))
		return
	}
	c.hub.Arm()
	c.sb.writeLine("  %s[ARM]%s Armed from console", cGreen, cReset)
}

func consoleDisarm(_ context.Context, c *console) {
	if !c.hub.IsArmed() {
		c.sb.writeLine("  Already disarmed")
		return
	}

	pin := ""
	if c.hub.PinRequired() {
		c.sb.writeLine("  PIN required to disarm. Type it and press enter:")
		if !c.in.Scan() {
			return
		}
		pin = strings.TrimSpace(c.in.Text())
	}

	if err := c.hub.DisarmWithPin("console", pin); err != nil {
		c.sb.writeLine("  %s[PIN]%s Refused: %v", cRed, cReset, err)
		return
	}
	c.sb.writeLine("  %s[DISARM]%s Disarmed from console", cGreen, cReset)
}

func consoleStatus(_ context.Context, c *console) {
	if c.hub.IsArmed() {
		c.sb.writeLine("  %sARMED%s since %s (%s ago)", cGreen, cReset,
			clockTime(c.hub.ArmedAt()),
			time.Since(c.hub.ArmedAt()).Round(time.Second))
	} else {
		c.sb.writeLine("  %sDISARMED%s", cDim, cReset)
	}
	c.sb.writeLine("  Phones connected: %d", c.hub.ClientCount())
	for _, s := range c.hub.GetSensorInfos() {
		c.sb.writeLine("    %-10s %s", s.Name, sensorMark(s))
	}
}

// sensorMark is the word the status listing puts next to a sensor's name.
func sensorMark(s ws.SensorInfo) string {
	switch {
	case !s.Available:
		return "unavailable"
	case s.Failure != "":
		// Named rather than folded into "on": this is the state the user most
		// needs to see and least expects to be in.
		return "FAILED — " + s.Failure
	case s.Enabled:
		return "on"
	default:
		return "off"
	}
}

func consoleHistory(_ context.Context, c *console) {
	evts, err := eventlog.ReadLast(filepath.Join(config.ConfigDir(), eventLogFileName), 20)
	switch {
	case err != nil:
		c.sb.writeLine("  No event history available")
	case len(evts) == 0:
		c.sb.writeLine("  No events recorded yet")
	default:
		for _, ev := range evts {
			ts := clockTime(ev.Timestamp)
			if ev.Sensor != "" {
				c.sb.writeLine("  %s [%s] %s — %s", ts, ev.Type, ev.Sensor, ev.Message)
			} else {
				c.sb.writeLine("  %s [%s] %s", ts, ev.Type, ev.Message)
			}
		}
	}
}

func consoleRotateKey(_ context.Context, c *console) {
	newKey, err := c.authMgr.Regenerate()
	if err != nil {
		c.sb.writeLine("  %s[ERROR]%s Failed to rotate key: %v", cRed, cReset, err)
		return
	}

	c.sb.setKey(newKey)
	c.sb.rekeyQR(c.authMgr.RawPairingKey())
	// Without this the stored key still holds the one just invalidated, and the
	// next start would come back with it.
	if c.sb.keyPath != "" {
		if err := auth.SaveKeyFile(c.sb.keyPath, c.authMgr.RawPairingKey()); err != nil {
			c.sb.writeLine("  %s[KEY]%s Rotated, but the stored key could not be updated: %v", cRed, cReset, err)
		}
	}
	c.sb.writeLine("  %s[KEY]%s Pairing key rotated. New key: %s%s%s", cGreen, cReset, cBold, newKey, cReset)
	c.sb.writeLine("  %s[KEY]%s All existing sessions invalidated. The QR code now encodes the new key.", cYellow, cReset)
}

func consoleURLs(_ context.Context, c *console) {
	urls, selected := c.sb.urlListWithSelection()
	for i, u := range urls {
		marker := " "
		if i == selected {
			marker = "*"
		}
		c.sb.writeLine("  %s[%d]%s %s%s", cBold, i+1, marker, u, cReset)
	}
	c.sb.writeLine("  %sUse 'qr <n>' to show the QR code for one of these.%s", cDim, cReset)
}

func consoleQR(_ context.Context, c *console) {
	n, err := strconv.Atoi(c.arg)
	if err != nil {
		c.sb.writeLine("  Usage: qr <n>   (type 'urls' to list them)")
		return
	}
	// Called once and held: showQR changes which code the dashboard is drawing,
	// so asking it twice would move the display on to answer its own question.
	shown := c.sb.showQR(n)
	if shown == "" {
		c.sb.writeLine("  No URL %d. Type 'urls' to list them.", n)
		return
	}
	c.sb.writeLine("  %s[QR]%s Now showing %s", cGreen, cReset, shown)
}

func consoleCert(_ context.Context, c *console) {
	fp := c.sb.certFingerprint()
	if fp == "" {
		c.sb.writeLine("  No TLS certificate — this server is running over plain HTTP on the local network.")
		return
	}
	c.sb.writeLine("  %sTLS certificate SHA-256 fingerprint:%s", cBold, cReset)
	c.sb.writeLine("  %s", fp)
	c.sb.writeLine("  %sYour phone will warn that this certificate is untrusted; it is self-signed.%s", cDim, cReset)
	c.sb.writeLine("  %sCompare the fingerprint above with the one the warning shows before accepting.%s", cDim, cReset)
}

func consoleUpdate(_ context.Context, c *console) {
	checkForUpdateNow(c.sb, c.hub, c.installMethod, c.updateLedger)
}

func consoleMode(ctx context.Context, c *console) {
	st := c.remoteCtl.State()
	cur := 1
	if st.Enabled {
		cur = 2
	}
	c.sb.writeLine("  [1] Wi-Fi only   [2] Remote access   (currently %d)", cur)
	c.sb.writeLine("  Type 1 or 2, or press enter to leave it alone:")
	if !c.in.Scan() {
		return
	}

	want, ok := parseModeChoice(c.in.Text())
	if !ok {
		c.sb.writeLine("  Left unchanged")
		return
	}
	if want == st.Enabled {
		c.sb.writeLine("  Already in that mode")
		return
	}

	// Written to disk before the listener moves, so a crash in between leaves
	// the config saying what the user asked for rather than what the process
	// happened to be doing.
	c.cfg.RemoteAccess = &want
	if err := config.Save(c.cfg); err != nil {
		c.sb.writeLine("  %s[NET]%s Could not save the setting: %v", cRed, cReset, err)
		return
	}

	next := c.remoteCtl.Disable()
	if want {
		next = c.remoteCtl.Enable(ctx)
	}
	applyRemoteState(c.sb, c.hub, c.srv, next)
	c.sb.writeLine("  %s[NET]%s Connection mode: %s", cGreen, cReset, connectionModeName(want))
}

func consoleLang(_ context.Context, c *console) {
	c.sb.writeLine("  [1] Türkçe   [2] English   (currently %s)", languageByCode(c.cfg.Language).code)
	c.sb.writeLine("  Type 1 or 2, or press enter to leave it alone:")
	if !c.in.Scan() {
		return
	}

	code, ok := parseLanguageChoice(c.in.Text())
	if !ok {
		c.sb.writeLine("  Left unchanged")
		return
	}

	c.cfg.Language = code
	if err := config.Save(c.cfg); err != nil {
		c.sb.writeLine("  %s[LANG]%s Could not save the setting: %v", cRed, cReset, err)
		return
	}
	// Said plainly rather than implied. The language reaches the two questions
	// asked before this dashboard exists and nothing on it, so a user who
	// changes it here and watches for the screen to turn would be waiting for
	// something that is never going to happen.
	c.sb.writeLine("  %s[LANG]%s Startup questions will be in %s from the next start.",
		cGreen, cReset, code)
}

func consoleHelp(_ context.Context, c *console) {
	c.sb.writeLine("  Commands: arm, disarm, status, stop, test, trigger <sensor>, history, urls, qr <n>, cert, mode, lang, update, rotate-key, help")
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

	applySensorPreferences(mgr, cfg.EnabledSensors)
}

// applySensorPreferences decides which sensors watch, from what the config
// recorded.
//
// Every sensor is on unless the config says otherwise. The manager registers
// them switched off, and this used to enable only the names the config marked
// true — so a fresh installation, whose config has no sensor map at all, armed
// with nothing watching. The dashboard said "0 / 6 active", the phone said "0
// sensors ready", and the user who read neither walked away from a laptop that
// was guarding itself against nothing.
//
// A recorded false is now applied rather than merely not-enabled, because with
// the default the other way round, skipping it would switch a sensor the user
// turned off back on at the next start.
func applySensorPreferences(mgr *monitor.Manager, prefs map[string]bool) {
	for _, s := range mgr.Sensors() {
		mgr.Enable(s.Name())
	}
	for name, enabled := range prefs {
		if enabled {
			mgr.Enable(name)
		} else {
			mgr.Disable(name)
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
