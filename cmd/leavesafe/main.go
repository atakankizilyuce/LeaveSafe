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
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/qr"
	"github.com/leavesafe/leavesafe/internal/server"
	"github.com/leavesafe/leavesafe/internal/ws"
)

var version = "dev"

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

	key  string
	urls []string
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
		midSep,
		sb.boxLine(fmt.Sprintf("  %s●%s  Key      %s%s%s", cYellow, cReset, cBold, sb.key, cReset)),
	}

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

func (sb *statusBar) doRedrawGrid() {
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
	sb.mu.Lock()
	defer sb.mu.Unlock()
	fmt.Fprintf(sb.out, "%s\n", fmt.Sprintf(format, args...))
	sb.doRedrawGrid()
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
	flag.Parse()

	log.SetFormatter(&log.TextFormatter{
		TimestampFormat: "15:04:05",
		FullTimestamp:   true,
	})

	maximizeConsole()
	time.Sleep(200 * time.Millisecond)

	cfg, err := config.Load()
	if err != nil {
		log.Warnf("Failed to load config, using defaults: %v", err)
	}

	authMgr, err := auth.NewManager()
	if err != nil {
		log.Fatalf("Failed to initialize auth: %v", err)
	}

	sensorMgr := monitor.NewManager()
	registerSensors(sensorMgr)

	hub := ws.NewHub(authMgr, sensorMgr, version)

	evLogPath := filepath.Join(config.ConfigDir(), "events.jsonl")
	if err := os.MkdirAll(config.ConfigDir(), 0o700); err != nil {
		log.Warnf("Failed to create config dir: %v", err)
	}
	if evLog, err := eventlog.New(evLogPath); err != nil {
		log.Warnf("Failed to open event log: %v", err)
	} else {
		hub.SetEventLogger(evLog)
		defer evLog.Close()
	}

	port := cfg.Port
	if v := os.Getenv("PORT"); v != "" {
		port, _ = strconv.Atoi(v)
	}

	srv := server.New(server.Config{Hub: hub, Port: port, DevMode: *devMode})
	if err := srv.Listen(); err != nil {
		log.Fatalf("Failed to bind port: %v", err)
	}

	sb := buildDashboard(os.Stdout, srv, authMgr, hub, sensorMgr)

	log.SetOutput(&logWriter{sb: sb})

	localAlarm := alarm.New(cfg.Alarm)

	hub.SetPinProtection(cfg.PinProtection.Enabled, cfg.PinProtection.Pin)
	hub.SetAutoArmOnLock(cfg.AutoArmOnLock)
	if cfg.AutoArmOnLock {
		sensorMgr.Enable("screen")
		sensorMgr.StartEnabled()
		log.Info("Auto-arm on screen lock enabled — screen sensor started")
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

	go hub.RunAlertDispatcher(ctx)
	go hub.RunHeartbeat(ctx)
	go runStatusTicker(ctx, sb)
	go runConsole(hub, sb, localAlarm, authMgr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
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

func buildDashboard(out *os.File, srv *server.Server, authMgr *auth.Manager,
	hub *ws.Hub, sensorMgr *monitor.Manager) *statusBar {

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

	qrVOff, statusVOff := 0, 0
	if len(qrLines) < totalRows {
		qrVOff = (totalRows - len(qrLines)) / 2
	}
	if statusH < totalRows {
		statusVOff = (totalRows - statusH) / 2
	}
	sb.gridRow = qrStartRow + statusVOff

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

	fmt.Fprintf(out, "\033[%d;1H\n", row)
	row++
	fmt.Fprintf(out, "\033[%d;1H  %sCommands:%s test, trigger <sensor>, stop, history, rotate-key, help  %s│%s  %sCtrl+C to quit%s\n",
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
			evts, err := eventlog.ReadLast(filepath.Join(config.ConfigDir(), "events.jsonl"), 20)
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
				sb.writeLine("  %s[KEY]%s Pairing key rotated. New key: %s%s%s", cGreen, cReset, cBold, newKey, cReset)
				sb.writeLine("  %s[KEY]%s All existing sessions invalidated.", cYellow, cReset)
			}
		case line == "help":
			sb.writeLine("  Commands: test, trigger <sensor>, stop, history, rotate-key, help")
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

func registerSensors(mgr *monitor.Manager) {
	mgr.Register(monitor.NewPowerSensor())
	mgr.Register(monitor.NewLidSensor())
	mgr.Register(monitor.NewUSBSensor())
	mgr.Register(monitor.NewScreenSensor())
	mgr.Register(monitor.NewNetworkSensor())
	mgr.Register(monitor.NewInputSensor())

	sensors := mgr.Sensors()
	available := 0
	for _, s := range sensors {
		if s.Available() {
			available++
			log.WithFields(log.Fields{"sensor": s.Name(), "display": s.DisplayName()}).Info("Sensor available")
		} else {
			log.WithFields(log.Fields{"sensor": s.Name(), "display": s.DisplayName()}).Info("Sensor unavailable")
		}
	}
	log.Infof("%d/%d sensors available", available, len(sensors))
}

