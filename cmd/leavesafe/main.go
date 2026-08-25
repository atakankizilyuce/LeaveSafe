package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/leavesafe/leavesafe/internal/alarm"
	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/eventlog"
	"github.com/leavesafe/leavesafe/internal/location"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/paircode"
	"github.com/leavesafe/leavesafe/internal/qr"
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
	// term is out again when out is a terminal, and nil when it is not. It is
	// kept so a repaint can ask how big the window is now rather than reuse the
	// size the first draw was laid out for. A test draws into a file and leaves
	// this nil, which is the same answer terminalSize gives for one.
	term *os.File

	// layout is where the last full draw put everything. Every repaint checks
	// it still describes the screen before painting a piece of it — see
	// layoutHolds.
	layout layout

	// line is what the user is typing, drawn on a row of its own at the foot of
	// the window. It is nil when nobody is typing on a screen this program
	// owns: a headless start, a -plain run, or input that is not a terminal —
	// and then the terminal echoes typed characters itself, as it always did.
	line *inputLine

	// qrCodes holds one rendered code per address, and qrURLIdx which of them
	// the box is showing. The box is sized to the largest of them so that
	// switching between them with `qr <n>` never overflows the layout.
	qrCodes  [][]string
	qrURLIdx int

	key string
	// rawKey is the pairing key as it goes into a QR code, without the grouping
	// that makes `key` readable on screen. The two are kept side by side
	// because rebuilding a QR code needs the raw form.
	rawKey string
	urls   []string

	// handedBack says the terminal is no longer the dashboard's to draw on.
	//
	// Shutting down gives it back before a word is printed, and the program
	// then keeps talking for a moment: the sensors say they stopped, the
	// server says it closed, a panic on the way out says so. Every one of
	// those goes through this writer, and drawn as the dashboard draws them
	// they carried the escapes that put the log back where the dashboard kept
	// it — and the repaint behind them painted the whole status grid over the
	// shell the user had just been handed back, leaving their cursor on a row
	// the dashboard chose.
	handedBack bool

	// headless drops every drawing operation. Started from an autostart entry
	// there is no terminal to draw on: the cursor-positioning escapes would go
	// into a log file as garbage, and the QR code would be a wall of blocks
	// nobody ever sees. Messages become ordinary log lines instead.
	headless bool
	// plainOut is where a -plain run prints, and nil for every other run. It is
	// the one shape with somebody watching and no dashboard to redraw, so it is
	// also the only one that has to print a new pairing code when the addresses
	// change rather than putting one back on screen.
	plainOut io.Writer
	// keyPath is where the pairing key is persisted, empty when it is not.
	// Rotating the key has to update that file, or the next start would come
	// back with the key the user just invalidated.
	keyPath string
}

// newHeadlessStatusBar returns a status bar that draws nothing, for runs with
// no terminal attached.
func newHeadlessStatusBar(hub *ws.Hub, sensorMgr *monitor.Manager, key, rawKey string,
	urls []string, keyPath string,
) *statusBar {
	return &statusBar{
		out:       io.Discard,
		hub:       hub,
		sensorMgr: sensorMgr,
		key:       key,
		rawKey:    rawKey,
		urls:      urls,
		headless:  true,
		keyPath:   keyPath,
		qrURLIdx:  -1,
	}
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

// keyText returns the pairing key in the grouped form the dashboard shows.
func (sb *statusBar) keyText() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.key
}

// pairCode is the short code the application's own pairing field takes, built
// from the address the QR box is currently showing.
//
// Two things go to a phone from this screen and they are not alternatives. The
// QR opens this machine's web client in a browser, which is the path for a
// phone with nothing installed. The code is typed into the application, which
// is the path for a phone that has it — and it exists because that field used
// to ask for an address and sixteen digits separately.
//
// Empty when there is nothing to build one from: no address reported yet, or an
// address that is a host name or IPv6 and cannot be carried. Nothing on screen
// beats a code that pairs with nothing.
func (sb *statusBar) pairCode() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.pairCodeLocked()
}

// pairCodeLocked is [pairCode] for a caller that already holds the mutex.
//
// gridLines is such a caller — refresh takes the lock and paints inside it — and
// a sync.Mutex is not reentrant, so taking it again there would hang the
// dashboard on every repaint rather than fail in a way anybody could see.
func (sb *statusBar) pairCodeLocked() string {
	idx := sb.qrURLIdx
	if idx < 0 || idx >= len(sb.urls) {
		return ""
	}

	u, err := url.Parse(sb.urls[idx])
	if err != nil {
		return ""
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return ""
	}

	code, err := paircode.Encode(u.Hostname(), port, sb.rawKey)
	if err != nil {
		return ""
	}
	return code
}

// setKey records a freshly rotated pairing key for the dashboard to draw.
func (sb *statusBar) setKey(key string) {
	sb.mu.Lock()
	sb.key = key
	sb.mu.Unlock()
	sb.refresh()
}

// visLen is how many columns a string takes on screen: its runes, not counting
// the color escapes, which are written to the terminal and occupy nothing.
func visLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if e := escapeLen(s, i); e > 0 {
			i += e
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		n++
		i += size
	}
	return n
}

// gridBoxWidth is how wide the status grid is drawn, never narrower than a box
// can be drawn at all.
//
// The floor is what keeps a grid asked for before any layout has been worked
// out — a test building a status bar by hand, a repaint that lost a race with
// the first draw — from being a box of width zero, which is not a narrow box
// but a crash.
func (sb *statusBar) gridBoxWidth() int {
	return max(sb.layout.gridWidth, floorGridWidth)
}

func (sb *statusBar) boxLine(content string) string {
	inner := sb.gridBoxWidth() - 2
	// Cut rather than allowed to overflow. A line wider than the box wraps onto
	// the next row, which pushes every row below it down while the layout still
	// believes they are where it put them.
	content = truncateVisible(content, inner)

	pad := inner - visLen(content)
	if pad < 0 {
		pad = 0
	}
	return "│" + content + strings.Repeat(" ", pad) + "│"
}

// gridHeight is how many rows the status grid takes.
//
// Answered by counting what is on offer rather than by drawing the grid and
// measuring it, because the layout asks this before every repaint — including
// the one behind every log line. Drawing it means asking each sensor whether it
// is available, and one of those answers by starting PowerShell.
func (sb *statusBar) gridHeight() int {
	// Border, title, separator, state, clients, sensors; then a separator, the
	// key, an address apiece and the bottom border.
	rows := 6 + 2 + len(sb.urls) + 1
	// And the pairing code, when there is one to draw. Counted here because
	// gridLines draws it on the same condition: the row was added to the grid
	// without being added to its height, so the layout gave the grid one row
	// less than it needed and drawGrid cut the bottom border off every window.
	if sb.pairCodeLocked() != "" {
		rows++
	}
	return rows
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

	w := sb.gridBoxWidth()
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

	lines = append(lines, midSep)
	lines = append(lines, sb.boxLine(fmt.Sprintf("  %s●%s  Key      %s%s%s", cYellow, cReset, cBold, sb.key, cReset)))

	// What the application's pairing field takes: one string carrying the
	// address and the key together, so nobody types two things. Drawn only
	// when there is an address to build it from — see pairCode.
	if code := sb.pairCodeLocked(); code != "" {
		lines = append(lines, sb.boxLine(
			fmt.Sprintf("  %s●%s  Code     %s%s%s", cYellow, cReset, cBold, code, cReset),
		))
	}

	maxURLVis := w - 2 - visLen("  ●  URL      ")
	for _, url := range sb.urls {
		lines = append(lines, sb.boxLine(
			fmt.Sprintf("  %s●%s  URL      %s%s%s", cGreen, cReset, cGreen,
				ellipsize(url, maxURLVis), cReset),
		))
	}

	lines = append(lines, bottom)
	return lines
}

// shownCode is the code the QR box is showing, or nothing when there is none
// to show: no addresses yet, a headless run, or an address whose code could not
// be rendered.
func (sb *statusBar) shownCode() []string {
	if sb.qrURLIdx < 0 || sb.qrURLIdx >= len(sb.qrCodes) {
		return nil
	}
	return sb.qrCodes[sb.qrURLIdx]
}

// drawQR repaints the reserved QR box with the currently selected code. The
// box is cleared first because codes for different URLs differ in size.
func (sb *statusBar) drawQR() {
	l := sb.layout
	if sb.headless || l.qrBoxH == 0 {
		return
	}
	blank := strings.Repeat(" ", l.qrBoxW)
	for i := range l.qrBoxH {
		fmt.Fprintf(sb.out, "\033[%d;%dH%s", l.qrRow+i, l.qrCol, blank)
	}
	if sb.qrURLIdx < 0 || sb.qrURLIdx >= len(sb.qrCodes) {
		return
	}
	for i, line := range sb.qrCodes[sb.qrURLIdx] {
		if i >= l.qrBoxH {
			break
		}
		fmt.Fprintf(sb.out, "\033[%d;%dH%s", l.qrRow+i, l.qrCol, line)
	}
}

// drawGrid repaints the status grid where the layout puts it, and no further
// down than the layout gave it room for.
func (sb *statusBar) drawGrid() {
	if sb.headless {
		return
	}
	for i, line := range sb.gridLines() {
		if i >= sb.layout.gridRows {
			break
		}
		fmt.Fprintf(sb.out, "\033[%d;%dH%s", sb.layout.gridRow+i, sb.layout.gridCol, line)
	}
}

// readsKeystrokes reports whether the keyboard is being read a keystroke at a
// time, which is what puts an input line of this program's own on screen.
func (sb *statusBar) readsKeystrokes() bool {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.line != nil
}

// promptRows is how much of the window the input line takes. It is held back
// from the layout, so that nothing else is placed on that row and the log's
// scrolling region stops above it.
func (sb *statusBar) promptRows() int {
	if sb.line == nil {
		return 0
	}
	return 1
}

// drawPrompt puts the input line back on its row and leaves the cursor in it.
//
// Called last by everything that writes to the screen. The cursor ending up
// under the user's fingers rather than wherever the last absolute move landed
// is what makes typing during a busy log survivable.
func (sb *statusBar) drawPrompt() {
	if sb.line == nil {
		return
	}
	row := sb.layout.termH + 1
	text, col := sb.line.view(sb.layout.termW)
	fmt.Fprintf(sb.out, "\033[%d;1H\033[2K%s\033[%d;%dH", row, text, row, col)
}

// applyKey gives one keystroke to the input line and draws the result.
func (sb *statusBar) applyKey(k key) action {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	a := sb.line.apply(k)
	sb.drawPrompt()
	return a
}

// takeLine hands back what was typed and clears the row for the next command.
func (sb *statusBar) takeLine() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	line := sb.line.take()
	sb.drawPrompt()
	return line
}

// maskLine draws what is typed as asterisks, or stops doing so.
func (sb *statusBar) maskLine(on bool) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.line.masked = on
	sb.drawPrompt()
}

// repaint puts one piece of the dashboard back on screen, or the whole thing
// when the layout it would be drawn against no longer describes the screen.
//
// Every partial repaint goes through here. Painting a piece at the row it used
// to be on, after something moved that row, is the whole of the duplicate
// status grid — and every caller below is a place where something could have
// moved: a phone connecting, an address arriving, the key being rotated, the
// window being resized under all of it.
func (sb *statusBar) repaint(piece func()) {
	if sb.headless || sb.handedBack {
		return
	}
	if !sb.layoutHolds() {
		sb.doPaint()
		return
	}

	if sb.line != nil {
		// Nothing to put aside: the cursor is on the input row and the saved
		// position belongs to the log, and a piece of the dashboard is drawn at
		// neither. Redrawing the input line is what puts the cursor back.
		piece()
		sb.drawPrompt()
		return
	}

	// The cursor is in the log, part-way through a line the user may be typing.
	// It is put back where it was rather than left wherever the last absolute
	// move landed.
	fmt.Fprint(sb.out, cursorSave)
	piece()
	fmt.Fprint(sb.out, cursorRestore)
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
	sb.repaint(sb.drawQR)
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
		lines, err := qr.Lines(pairingURL(u, rawKey))
		if err != nil {
			continue
		}
		sb.qrCodes[i] = lines
	}
	sb.repaint(sb.drawQR)
}

func (sb *statusBar) doRedrawGrid() {
	sb.repaint(sb.drawGrid)
}

func (sb *statusBar) refresh() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.doRedrawGrid()
}

// handBack stops the dashboard drawing, for the moment the terminal stopped
// being the dashboard's.
//
// It does not stop the program talking — the shutdown has things to say and the
// user is entitled to read them. It makes them ordinary lines: no absolute
// positioning, no cursor store, no repaint, printed one under the other into
// the shell the way any other command's output is.
func (sb *statusBar) handBack() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.handedBack = true
	// The log's position lived in the terminal's cursor store while there was
	// an input row below it. There is no input row now, and no store to put
	// anything back from.
	sb.line = nil
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
	sb.writeLog(fmt.Sprintf(format, args...))
	sb.doRedrawGrid()
}

// writeLog puts one line into the scrolling region.
//
// With an input line on screen the cursor is not in the log — it is on the row
// the user is typing on, which is below the scrolling region and must not
// scroll. So the log's own position is kept in the terminal's cursor store:
// restored to write, saved again afterwards, and the input line redrawn last by
// the repaint that follows. Written at the cursor instead, every log line would
// land on top of whatever was half-typed.
func (sb *statusBar) writeLog(text string) {
	if sb.line == nil {
		fmt.Fprintf(sb.out, "%s\n", text)
		return
	}
	fmt.Fprintf(sb.out, "%s%s\n%s", cursorRestore, text, cursorSave)
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

// ensureLanguageChoice asks the language question on an interactive start and
// remembers the answer.
//
// A headless start has nobody to answer and blocking on stdin there would hang
// the service forever, so it takes what is stored. Every autostart entry this
// program writes passes -headless, so no unattended start can reach the prompt.
func ensureLanguageChoice(cfg *config.Config, headless bool) {
	if headless {
		return
	}
	ensureLanguage(cfg)
}

// pairingURL builds the address a QR code encodes: the server and the pairing
// key.
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
func pairingURL(base, rawKey string) string {
	return base + "/#key=" + rawKey
}

// logHeadlessStartup writes what the dashboard would have shown.
//
// Started from an autostart entry there is no screen and no QR code, so the log
// file is the only place the user can find the address to open and the
// certificate to check. Not the pairing key: that is in its own owner-only file
// rather than in a log that gets pasted into bug reports.
//
// mode names why there is no dashboard, because there are now two reasons and
// they are not the same thing to a reader of the log: nobody is watching a
// headless start, and somebody is watching a -plain one.
func logHeadlessStartup(sb *statusBar, mode string) {
	log.Infof("LeaveSafe %s started %s — no dashboard on this run", version, mode)
	for _, u := range sb.urlList() {
		log.Infof("Reachable at %s", u)
	}
	if sb.keyPath != "" {
		log.Infof("Pairing key is stored in %s — it is not written to this log", sb.keyPath)
	}
}

// printPairingCode writes a QR code, the address it encodes and the pairing key
// to the terminal, once, as ordinary scrolling output.
//
// This is what -plain has instead of a dashboard. The pairing key is written
// straight to the terminal rather than logged, because the log is a file that
// gets attached to bug reports and the key is the whole of the front door.
func printPairingCode(out io.Writer, sb *statusBar, rawKey string) {
	urls := sb.urlList()
	if len(urls) == 0 {
		return
	}

	fmt.Fprintf(out, "\n  %sScan to connect:%s\n\n", cDim, cReset)
	lines, err := qr.Lines(pairingURL(urls[0], rawKey))
	if err != nil {
		fmt.Fprintf(out, "  %sNo QR code for %s: %v — open the address by hand.%s\n",
			cYellow, urls[0], err, cReset)
	}
	for _, line := range lines {
		fmt.Fprintf(out, "  %s\n", line)
	}

	fmt.Fprintf(out, "\n  %s●%s  URL      %s%s%s\n", cGreen, cReset, cGreen, urls[0], cReset)
	fmt.Fprintf(out, "  %s●%s  Key      %s%s%s\n", cYellow, cReset, cBold, sb.keyText(), cReset)
	fmt.Fprintf(out, "\n  %sCommands: type 'help'. Ctrl+C to quit.%s\n\n", cDim, cReset)
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

func buildDashboard(out *os.File, in io.Reader, srv *server.Server, authMgr *auth.Manager,
	hub *ws.Hub, sensorMgr *monitor.Manager,
) *statusBar {
	urls := srv.URLs()

	sb := &statusBar{
		out:       out,
		term:      out,
		hub:       hub,
		sensorMgr: sensorMgr,
		qrCodes:   renderQRCodes(urls, authMgr.RawPairingKey()),
		key:       authMgr.PairingKey(),
		rawKey:    authMgr.RawPairingKey(),
		urls:      urls,
	}

	// The dashboard clears the screen, draws at absolute positions and pins a
	// scrolling region. Doing that on the screen the user was already using
	// destroys their scrollback, so it is done on the alternate one — which is
	// also what makes handing the terminal back at the end a single escape
	// rather than an attempt to put everything the way it was.
	//
	// The keyboard goes with it, when there is a keyboard to take. That has to
	// be settled before the first draw rather than after: an input line of this
	// program's own needs a row of the window that nothing else may be placed
	// on, and the layout is worked out here.
	keys, _ := in.(*os.File)
	terminalScreen.readsKeys(keys)
	terminalScreen.enter(out)
	if terminalScreen.takesKeystrokes() {
		sb.line = &inputLine{}
	}
	sb.paint()

	return sb
}

// paint draws the whole dashboard: banner, QR box, status grid, footer, and the
// scrolling region the log lines live in below them.
//
// Separate from buildDashboard because the first draw is no longer the only
// one. Coming back from Ctrl+Z lands on an alternate screen the terminal
// cleared on the way in, and nothing about the dashboard survives that.
func (sb *statusBar) paint() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.doPaint()
}

// doPaint is paint with the lock already held, which is how the drawing helpers
// below expect to be called: none of them takes it.
func (sb *statusBar) doPaint() {
	if sb.headless || sb.handedBack {
		return
	}

	out := sb.out
	l := sb.wantedLayout()
	sb.layout = l

	fmt.Fprintf(out, "\033[2J\033[H")
	drawHeader(out, l)
	drawScanLabel(out, l)
	sb.drawQR()
	sb.drawGrid()
	drawFooter(out, l)

	// Everything above is drawn once and must stay put, so the scrolling region
	// starts below it. Without this the log would scroll over the QR code the
	// user is trying to scan. It stops above the input line for the same
	// reason: a row that scrolled would take what the user is typing with it.
	fmt.Fprintf(out, "\033[%d;%dr", l.logRow, l.termH)
	fmt.Fprintf(out, "\033[%d;1H", l.logRow)
	// The log starts here, and from here on its position lives in the cursor
	// store rather than in the cursor — which belongs to the input line.
	if sb.line != nil {
		fmt.Fprint(out, cursorSave)
	}
	sb.drawPrompt()
}

// wantedLayout is where everything would go if the dashboard were drawn now,
// against the window as it is now and the state as it is now.
func (sb *statusBar) wantedLayout() layout {
	termW, termH := terminalSize(sb.term)
	qrW, qrH := qrBoxSize(sb.shownCode())
	// The input line's row is taken off the window before anything is placed
	// in it, so that the layout — which knows nothing about typing — cannot put
	// the log's last row where the user is writing.
	return computeLayout(termW, termH-sb.promptRows(), qrW, qrH, sb.gridHeight())
}

// layoutHolds reports whether the screen still looks the way the last full draw
// left it.
//
// This is the check that stops a repaint drawing a second copy of something.
// The pieces below paint at absolute rows, and two things move those rows out
// from under them: the window being resized, and a longer address producing a
// bigger QR code. Both used to leave the old drawing on screen with the new one
// painted somewhere else — which is what the two status grids were.
func (sb *statusBar) layoutHolds() bool {
	return sb.wantedLayout() == sb.layout
}

// terminalSize is the size of the terminal, or a shape to lay out for when
// there is no terminal to ask.
//
// Nothing to ask means a test drawing into a buffer or a headless start, and a
// size has to come from somewhere. A real window is taken at its word however
// small it is: the layout adapts, and inventing a larger one for it is what put
// the log's scrolling region over the top of the status grid — every line
// written then scrolled the grid away and the next repaint drew it again lower
// down, two grids on one screen.
func terminalSize(out *os.File) (width, height int) {
	if out == nil {
		return 120, 40
	}
	w, h, err := termSizeFn(int(out.Fd()))
	if err != nil || w < 1 || h < 1 {
		return 120, 40
	}
	return w, h
}

// termSizeFn is term.GetSize, as a variable so a test can answer for a window
// no test process has: `go test` is attached to a pipe, and every size it could
// be asked for is the failure the fallback above is for. Nothing in the running
// program reassigns it.
var termSizeFn = term.GetSize

// bannerLines is the name in block letters, which is bannerWidth columns wide
// and does not shrink.
var bannerLines = []string{
	"  ██╗     ███████╗ █████╗ ██╗   ██╗███████╗███████╗ █████╗ ███████╗███████╗",
	"  ██║     ██╔════╝██╔══██╗██║   ██║██╔════╝██╔════╝██╔══██╗██╔════╝██╔════╝",
	"  ██║     █████╗  ███████║██║   ██║█████╗  ███████╗███████║█████╗  █████╗  ",
	"  ██║     ██╔══╝  ██╔══██║╚██╗ ██╔╝██╔══╝  ╚════██║██╔══██║██╔══╝  ██╔══╝  ",
	"  ███████╗███████╗██║  ██║ ╚████╔╝ ███████╗███████║██║  ██║██║     ███████╗",
	"  ╚══════╝╚══════╝╚═╝  ╚═╝  ╚═══╝  ╚══════╝╚══════╝╚═╝  ╚═╝╚═╝     ╚══════╝",
}

// drawHeader writes the banner, the version and the rule under them, in the
// number of rows the layout set aside for it.
//
// It does not decide that for itself, and the two things that make it give way
// are different in kind: a window too narrow for the block letters cannot have
// them at all, and a window too short for everything gives them up so the QR
// code can stay. The second is only knowable once the code has been placed, so
// the layout owns the answer — and a header that drew more rows than the layout
// allowed put its own banner underneath whatever was placed below it.
func drawHeader(out io.Writer, l layout) {
	row := 1
	if l.headRows >= bannerRows {
		for _, line := range bannerLines {
			fmt.Fprintf(out, "\033[%d;1H%s%s%s", row, cCyan, line, cReset)
			row++
		}
	}
	fmt.Fprintf(out, "\033[%d;1H%s", row,
		truncateVisible(fmt.Sprintf("  %s%s%s  %sDevice Security Monitor%s",
			cBold, version, cReset, cDim, cReset), l.termW))
	row++
	fmt.Fprintf(out, "\033[%d;1H%s%s%s", row, cDim, rule(l.termW), cReset)
}

// renderQRCodes builds a QR code for every address the laptop can be reached at,
// not just the first.
//
// A laptop on both Wi-Fi and Ethernet has an address on each, and only one of
// them is the network the phone is on. `qr <n>` switches between them.
//
// A code that will not render becomes a nil entry rather than a missing one, so
// the indexes stay lined up with the address list they came from.
func renderQRCodes(urls []string, rawKey string) [][]string {
	codes := make([][]string, 0, len(urls))
	for _, u := range urls {
		lines, err := qr.Lines(pairingURL(u, rawKey))
		if err != nil {
			log.Warnf("Could not render QR code for %s: %v", u, err)
			lines = nil
		}
		codes = append(codes, lines)
	}
	return codes
}

// qrBoxSize is the size of the code the dashboard is showing.
//
// It used to be the size of the largest of them, so that switching between them
// with `qr <n>` could not overflow a box laid out for a smaller one. That cost
// the window: boxing the code somebody is looking at at another one's size is
// how a window with room for it was told it had none. A switch now
// takes the layout with it, which is safe because every repaint already checks
// that the layout still describes the screen.
func qrBoxSize(lines []string) (width, height int) {
	if len(lines) == 0 {
		return 0, 0
	}
	return utf8.RuneCountInString(lines[0]), len(lines)
}

// drawScanLabel writes the line above the QR box.
//
// With no room for a code it says so rather than leaving a gap. A QR code
// cannot be made smaller, so a window this narrow simply has none — and a user
// looking for something to scan deserves to be told that, and told what to do
// about it, instead of hunting for a code that was never drawn. The addresses
// are still listed in the grid.
func drawScanLabel(out io.Writer, l layout) {
	label := fmt.Sprintf("  %sScan to connect:%s", cDim, cReset)
	if l.qrBoxH == 0 {
		label = fmt.Sprintf("  %sNo room for a QR code — widen the window, "+
			"or open an address below.%s", cYellow, cReset)
	}
	fmt.Fprintf(out, "\033[%d;1H%s", l.labelRow, truncateVisible(label, l.termW))
}

// drawFooter writes the command list and the rule under it, in the two rows
// above where the log begins.
//
// The command list is cut to the window rather than allowed to wrap. A wrapped
// footer pushes the first log line down a row, and the row it pushes it onto is
// inside the scrolling region the layout just pinned — so the screen ends up one
// line out of step with what the program believes it drew.
func drawFooter(out io.Writer, l layout) {
	if !l.footerShown {
		return
	}

	// The ones worth reaching for without being told, and then `help` for the
	// rest. The full list came to a hundred and thirty-four columns, which is
	// wider than the window most people run this in — so it wrapped, and a
	// wrapped footer is a row of the log that the layout does not know it lost.
	commands := fmt.Sprintf("  %sCommands:%s arm, disarm, status, stop, test, trigger <sensor>, "+
		"qr <n>, urls, help  %s│%s  %sCtrl+C to quit%s",
		cDim, cReset, cDim, cReset, cDim, cReset)

	fmt.Fprintf(out, "\033[%d;1H%s", l.logRow-2, truncateVisible(commands, l.termW))
	fmt.Fprintf(out, "\033[%d;1H%s%s%s", l.logRow-1, cDim, rule(l.termW), cReset)
}

// rule is the horizontal line under the banner and above the log.
func rule(termW int) string {
	return "  " + strings.Repeat("─", max(termW-4, 1))
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
	cfg           *config.Config
	// quit ends the program the way Ctrl+C used to. In raw mode the terminal
	// no longer turns that keystroke into a signal on our behalf, so the loop
	// has to be able to do it itself. Nil everywhere the terminal is still
	// doing it.
	quit func()
}

// console is the state one typed command acts on: the dependencies, plus the
// input the three commands that ask a follow-up question read their answer from.
//
// The context is not in here. It belongs to the run of a single command rather
// than to the loop, and a context parked in a struct outlives the call it was
// meant to bound.
type console struct {
	consoleDeps
	in lineReader
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
	"update":     {run: consoleUpdate},
	"lang":       {run: consoleLang},
	"help":       {run: consoleHelp},
}

// runConsole reads typed commands until the reader is exhausted. The reader is
// a parameter rather than os.Stdin directly so the loop can be driven from a
// test; the running program always passes os.Stdin.
func runConsole(ctx context.Context, in io.Reader, d consoleDeps) {
	c := &console{consoleDeps: d, in: newLineReader(bufio.NewReader(in), d.sb, d.quit)}

	for {
		typed, ok := c.in.readLine()
		if !ok {
			return
		}
		line := strings.TrimSpace(typed)
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
		typed, ok := c.in.readSecret()
		if !ok {
			return
		}
		pin = strings.TrimSpace(typed)
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

func consoleUpdate(_ context.Context, c *console) {
	checkForUpdateNow(c.sb, c.hub, c.installMethod, c.updateLedger)
}

func consoleLang(_ context.Context, c *console) {
	c.sb.writeLine("  [1] Türkçe   [2] English   (currently %s)", languageByCode(c.cfg.Language).code)
	c.sb.writeLine("  Type 1 or 2, or press enter to leave it alone:")
	typed, ok := c.in.readLine()
	if !ok {
		return
	}

	code, ok := parseLanguageChoice(typed)
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
	// Every one of them, because this is the list the footer's shorter one
	// points at.
	c.sb.writeLine("  Commands: arm, disarm, status, stop, test, trigger <sensor>, history, urls, qr <n>, lang, update, rotate-key, help")
	// Said out loud because the keys are this program's own now: on a dashboard
	// it reads the keyboard itself, and nothing about the row being typed on
	// suggests that the last command is one press away.
	if c.sb.readsKeystrokes() {
		c.sb.writeLine("  %sType on the line at the foot of the window. ↑ and ↓ walk through what you typed before.%s", cDim, cReset)
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
