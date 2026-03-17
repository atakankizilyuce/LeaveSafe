package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/qr"
	"github.com/leavesafe/leavesafe/internal/server"
	"github.com/leavesafe/leavesafe/internal/ws"
)

// ANSI color / style codes
const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cCyan   = "\033[36m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cRed    = "\033[31m"
)

// ── Status grid ──────────────────────────────────────────────────────────────

// statusBar owns the status grid drawn to the right of the QR code.
// It updates that region in-place without touching the scroll area.
type statusBar struct {
	mu        sync.Mutex
	hub       *ws.Hub
	sensorMgr *monitor.Manager
	out       io.Writer

	gridRow   int // terminal row of the top border (1-indexed)
	gridCol   int // terminal column of the left border (1-indexed)
	gridWidth int // total visual width including border chars

	key  string
	urls []string
}

// visLen returns the visible (column) width of s, ignoring ANSI escape sequences.
func visLen(s string) int {
	n, i := 0, 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' {
				i++
			}
			if i < len(s) {
				i++ // skip 'm'
			}
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		n++
		i += size
	}
	return n
}

// boxLine pads content to fill the inner width of the grid and adds side borders.
func (sb *statusBar) boxLine(content string) string {
	inner := sb.gridWidth - 2
	pad := inner - visLen(content)
	if pad < 0 {
		pad = 0
	}
	return "│" + content + strings.Repeat(" ", pad) + "│"
}

// gridLines builds every line of the status grid with current runtime values.
func (sb *statusBar) gridLines() []string {
	// Armed state
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
		midSep,
		sb.boxLine(fmt.Sprintf("  %s●%s  Key      %s%s%s", cYellow, cReset, cBold, sb.key, cReset)),
	}

	// URL lines — truncate to fit inside the box
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

// doRedrawGrid redraws the status grid at its fixed position (caller holds mu or is at init).
func (sb *statusBar) doRedrawGrid() {
	lines := sb.gridLines()
	fmt.Fprintf(sb.out, "\033[s") // save cursor
	for i, line := range lines {
		fmt.Fprintf(sb.out, "\033[%d;%dH%s", sb.gridRow+i, sb.gridCol, line)
	}
	fmt.Fprintf(sb.out, "\033[u") // restore cursor
}

func (sb *statusBar) refresh() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.doRedrawGrid()
}

func (sb *statusBar) writeLine(format string, args ...interface{}) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	fmt.Fprintf(sb.out, "%s\n", fmt.Sprintf(format, args...))
	sb.doRedrawGrid()
}

// logWriter routes log output through the status bar so it scrolls cleanly.
type logWriter struct{ sb *statusBar }

func (w *logWriter) Write(p []byte) (n int, err error) {
	if msg := strings.TrimRight(string(p), "\n"); msg != "" {
		w.sb.writeLine("  %s", msg)
	}
	return len(p), nil
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile)

	// Maximize the console window before drawing anything.
	maximizeConsole()
	time.Sleep(200 * time.Millisecond)

	authMgr, err := auth.NewManager()
	if err != nil {
		log.Fatalf("Failed to initialize auth: %v", err)
	}

	sensorMgr := monitor.NewManager()
	registerSensors(sensorMgr)

	hub := ws.NewHub(authMgr, sensorMgr)

	port := 0
	if v := os.Getenv("PORT"); v != "" {
		port, _ = strconv.Atoi(v)
	}

	srv := server.New(server.Config{Hub: hub, Port: port})
	if err := srv.Listen(); err != nil {
		log.Fatalf("Failed to bind port: %v", err)
	}

	// Draw the full dashboard, set up scroll region, return the live status bar.
	sb := buildDashboard(os.Stdout, srv, authMgr, hub, sensorMgr)

	log.SetOutput(&logWriter{sb: sb})

	hub.SetDisconnectCallback(func() {
		log.Println("[ALARM] All clients disconnected while armed — local alarm triggered.")
		triggerLocalAlarm()
	})
	hub.SetClientChangeCallback(func(_ int, _ bool) {
		sb.refresh()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.RunAlertDispatcher(ctx)
	go hub.RunHeartbeat(ctx)
	go runStatusTicker(ctx, sb)
	go runConsole(hub, sb)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		// Reset scroll region so the terminal is clean after exit.
		fmt.Fprintf(os.Stdout, "\033[r\033[?25h\n")
		fmt.Printf("  %sShutting down…%s\n", cDim, cReset)
		cancel()
		sensorMgr.StopAll()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
		os.Exit(0)
	}()

	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// ── Dashboard layout ──────────────────────────────────────────────────────────

// buildDashboard clears the terminal, draws the static header (banner + QR + status
// grid side by side), sets the VT scroll region to the area below the header, and
// returns the statusBar ready for in-place refreshes.
func buildDashboard(out *os.File, srv *server.Server, authMgr *auth.Manager,
	hub *ws.Hub, sensorMgr *monitor.Manager) *statusBar {

	termW, termH, err := term.GetSize(int(out.Fd()))
	if err != nil || termW < 80 || termH < 20 {
		termW, termH = 120, 40
	}

	// Clear screen and move cursor to top-left.
	fmt.Fprintf(out, "\033[2J\033[H")

	row := 1 // tracks the next row to write (1-indexed)

	// ── ASCII banner (6 lines) ────────────────────────────────────────────────
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
	fmt.Fprintf(out, "  %sv1.0%s  %sDevice Security Monitor%s\n", cBold, cReset, cDim, cReset)
	row++

	sep := "  " + strings.Repeat("─", termW-4)
	fmt.Fprintf(out, "%s%s%s\n", cDim, sep, cReset)
	row++

	fmt.Fprintf(out, "\n") // blank line
	row++

	// ── QR code + status grid side by side ───────────────────────────────────
	urls := srv.URLs()
	qrURL := ""
	if len(urls) > 0 {
		qrURL = urls[0] + "?key=" + authMgr.RawPairingKey()
	}

	qrLines, _ := qr.Lines(qrURL)

	const qrIndent = 2
	qrW := 0
	if len(qrLines) > 0 {
		qrW = utf8.RuneCountInString(qrLines[0])
	}

	// Status grid is placed gap chars to the right of the QR code.
	const gap = 3
	statusCol := qrIndent + qrW + gap + 1 // +1: columns are 1-indexed
	statusW := termW - statusCol - 1      // right margin 1
	if statusW > 50 {
		statusW = 50
	}
	if statusW < 30 {
		statusW = 30
	}

	// "Scan to connect:" label on the left, before the QR+grid block.
	fmt.Fprintf(out, "  %sScan to connect:%s\n", cDim, cReset)
	row++

	qrStartRow := row

	sb := &statusBar{
		out:       out,
		hub:       hub,
		sensorMgr: sensorMgr,
		gridRow:   qrStartRow,
		gridCol:   statusCol,
		gridWidth: statusW,
		key:       authMgr.PairingKey(),
		urls:      urls,
	}

	statusLines := sb.gridLines()
	statusH := len(statusLines)

	totalRows := len(qrLines)
	if statusH > totalRows {
		totalRows = statusH
	}

	// Vertically center the shorter column within the taller one.
	qrVOff, statusVOff := 0, 0
	if len(qrLines) < totalRows {
		qrVOff = (totalRows - len(qrLines)) / 2
	}
	if statusH < totalRows {
		statusVOff = (totalRows - statusH) / 2
	}
	sb.gridRow = qrStartRow + statusVOff // real row where the grid top border sits

	// Render QR and status using absolute cursor positioning so they don't
	// interfere with each other or trigger unexpected scrolling.
	for i := 0; i < totalRows; i++ {
		fmt.Fprintf(out, "\033[%d;1H", qrStartRow+i)

		qi := i - qrVOff
		if qi >= 0 && qi < len(qrLines) {
			fmt.Fprintf(out, "%s%s", strings.Repeat(" ", qrIndent), qrLines[qi])
		}

		si := i - statusVOff
		if si >= 0 && si < len(statusLines) {
			fmt.Fprintf(out, "\033[%d;%dH%s", qrStartRow+i, statusCol, statusLines[si])
		}
	}

	row = qrStartRow + totalRows

	// ── Footer ────────────────────────────────────────────────────────────────
	fmt.Fprintf(out, "\033[%d;1H\n", row)
	row++
	fmt.Fprintf(out, "\033[%d;1H  %sCommands:%s test, help  %s│%s  %sCtrl+C to quit%s\n",
		row, cDim, cReset, cDim, cReset, cDim, cReset)
	row++
	fmt.Fprintf(out, "\033[%d;1H%s%s%s\n", row, cDim, sep, cReset)
	row++

	// ── Scroll region ─────────────────────────────────────────────────────────
	// Reserve rows (row..termH) as the live log area.  The header above is
	// outside the scroll region and will never be scrolled away.
	headerRows := row - 1
	if headerRows > termH-3 {
		headerRows = termH - 3
	}
	fmt.Fprintf(out, "\033[%d;%dr", headerRows+1, termH) // set scroll region
	fmt.Fprintf(out, "\033[%d;1H", headerRows+1)         // move cursor into it

	return sb
}

// ── Background helpers ────────────────────────────────────────────────────────

func runConsole(hub *ws.Hub, sb *statusBar) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch line := strings.TrimSpace(scanner.Text()); line {
		case "test":
			hub.PushAlert(ws.NewAlert("test", "warning", "Test alert from console"))
			sb.writeLine("  %s[TEST]%s Alert sent to %d client(s)", cYellow, cReset, hub.ClientCount())
		case "help":
			sb.writeLine("  Commands: test, help")
		case "":
			// ignore empty input
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

// ── Sensor registration ───────────────────────────────────────────────────────

func registerSensors(mgr *monitor.Manager) {
	mgr.Register(monitor.NewPowerSensor())
	mgr.Register(monitor.NewLidSensor())
	mgr.Register(monitor.NewUSBSensor())
	mgr.Register(monitor.NewScreenSensor())
	mgr.Register(monitor.NewNetworkSensor())

	sensors := mgr.Sensors()
	available := 0
	for _, s := range sensors {
		if s.Available() {
			available++
			log.Printf("[INFO] Sensor available: %s (%s)", s.Name(), s.DisplayName())
		} else {
			log.Printf("[INFO] Sensor unavailable: %s (%s)", s.Name(), s.DisplayName())
		}
	}
	log.Printf("[INFO] %d/%d sensors available", available, len(sensors))
}

// ── Alarm ─────────────────────────────────────────────────────────────────────

func triggerLocalAlarm() {
	for range 10 {
		fmt.Print("\a")
		time.Sleep(500 * time.Millisecond)
	}
}
