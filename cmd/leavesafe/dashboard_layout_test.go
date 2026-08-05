package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/remote"
	"github.com/leavesafe/leavesafe/internal/ws"
)

// The dashboard is drawn once, at a fixed position, and then never redrawn as a
// whole — the status grid repaints in place and log lines scroll underneath it.
// That makes the layout arithmetic load-bearing rather than cosmetic: a QR box
// sized to the wrong code is a code cut in half, and a scrolling region that
// starts too high is a log scrolling over the thing the user is trying to scan.

// drawnDashboard builds the dashboard into a file and hands back both the status
// bar and everything that was written.
//
// A file rather than a terminal, which is also the interesting case: term.GetSize
// cannot answer for one, so this exercises the fallback shape every non-tty run
// gets.
func drawnDashboard(t *testing.T, remoteState remote.State) (*statusBar, string) {
	t.Helper()

	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	path := filepath.Join(t.TempDir(), "screen")
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create the screen file: %v", err)
	}
	t.Cleanup(func() { _ = out.Close() })

	mgr := monitor.NewManager()
	hub := ws.NewHub(authMgr, mgr, "test")
	srv := testServer(t)

	sb := buildDashboard(out, srv, authMgr, hub, mgr, remoteState)

	if err := out.Sync(); err != nil {
		t.Fatalf("flush the screen file: %v", err)
	}
	drawn, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the screen file back: %v", err)
	}
	return sb, string(drawn)
}

func TestDashboardDrawsItselfAndHandsBackAStatusBarThatMatches(t *testing.T) {
	sb, drawn := drawnDashboard(t, remote.State{})

	if !strings.Contains(drawn, "Device Security Monitor") {
		t.Errorf("the banner is missing; screen was:\n%s", drawn)
	}
	if !strings.Contains(drawn, "Scan to connect:") {
		t.Errorf("nothing invited the user to scan; screen was:\n%s", drawn)
	}
	if !strings.Contains(drawn, "Ctrl+C to quit") {
		t.Errorf("the command footer is missing; screen was:\n%s", drawn)
	}

	// The status bar is what every later repaint draws through, so what it holds
	// has to match what was just put on screen.
	if sb.headless {
		t.Error("a dashboard was drawn but its status bar reports itself headless")
	}
	if len(sb.urlList()) == 0 {
		t.Error("the dashboard offers no address to pair with")
	}
	if len(sb.qrCodes) != len(sb.urlList()) {
		t.Errorf("%d QR codes for %d addresses — the indexes qr <n> uses are out of step",
			len(sb.qrCodes), len(sb.urlList()))
	}
}

// The scrolling region has to begin below everything drawn once, or the log
// scrolls over the QR code. It is the last escape written, and it names the row
// the scrolling starts on.
func TestDashboardKeepsTheLogBelowWhatItDrewOnce(t *testing.T) {
	sb, drawn := drawnDashboard(t, remote.State{})

	bottom := sb.qrRow + sb.qrBoxH
	found := false
	for row := bottom; row < 40; row++ {
		if strings.Contains(drawn, "\033["+itoa(row)+";40r") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no scrolling region starts below the QR box (which ends at row %d)", bottom-1)
	}
}

// The certificate rides in the QR code so the phone can check which server it
// reached before offering the pairing key. A dashboard that drew the codes
// before the certificate was known would hand out codes that skip the check.
func TestDashboardCarriesTheCertificateIntoTheStatusBar(t *testing.T) {
	const fp = "AA:BB:CC:DD:EE:FF"
	sb, _ := drawnDashboard(t, remote.State{Enabled: true, CertFP: fp})

	if got := sb.certFingerprint(); got != fp {
		t.Errorf("certFingerprint = %q, want %q", got, fp)
	}
}

// Remote access adds a public address, and the dashboard has to offer it — it is
// the only one a phone on another network can use.
func TestDashboardOffersThePublicAddressWhenThereIsOne(t *testing.T) {
	sb, _ := drawnDashboard(t, remote.State{
		Enabled:   true,
		UPnP:      remote.UPnPOK,
		PublicURL: "https://198.51.100.4:9443",
	})

	urls := sb.urlList()
	if len(urls) == 0 || urls[0] != "https://198.51.100.4:9443" {
		t.Errorf("the public address is not the one the QR code shows: %v", urls)
	}
	if !strings.Contains(sb.remoteStatus, "ACTIVE") {
		t.Errorf("remoteStatus = %q, want it to report remote access as active", sb.remoteStatus)
	}
}

// A file is not a terminal, so its size cannot be asked for. Falling back is not
// a detail: the layout assumes it has room, and squeezing it into a window too
// small produces a QR code cut in half, which is worse than one running off the
// edge of a window the user can resize.
func TestTerminalSizeFallsBackWhenItCannotAsk(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "not-a-terminal"))
	if err != nil {
		t.Fatalf("create the file: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	w, h := terminalSize(f)

	if w != 120 || h != 40 {
		t.Errorf("terminalSize = %dx%d, want the 120x40 fallback", w, h)
	}
}

// The box is sized to the largest code so that switching between addresses with
// `qr <n>` never overflows into the rest of the layout.
func TestQRBoxIsSizedToTheLargestCode(t *testing.T) {
	codes := [][]string{
		{"##", "##"},
		{"####", "####", "####"},
		{"###"},
	}

	w, h := qrBoxSize(codes)

	if w != 4 || h != 3 {
		t.Errorf("qrBoxSize = %dx%d, want 4x3 — the largest of the three", w, h)
	}
}

// A code that would not render is kept as a nil entry rather than dropped, so
// the indexes stay lined up with the address list. It must not be mistaken for
// the widest one either.
func TestQRBoxIgnoresACodeThatWouldNotRender(t *testing.T) {
	w, h := qrBoxSize([][]string{nil, {"###", "###"}})

	if w != 3 || h != 2 {
		t.Errorf("qrBoxSize = %dx%d, want 3x2", w, h)
	}
}

func TestQRBoxOfNothingIsEmpty(t *testing.T) {
	if w, h := qrBoxSize(nil); w != 0 || h != 0 {
		t.Errorf("qrBoxSize = %dx%d, want 0x0", w, h)
	}
}

// The status column sits to the right of the QR box, and its width is clamped at
// both ends: too narrow and the sensor lines wrap into nonsense, too wide and it
// sprawls across a maximized window with a yard of space between the columns.
func TestStatusColumnIsPlacedBesideTheCodeAndKeptReadable(t *testing.T) {
	cases := map[string]struct {
		termW, qrW  int
		wantCol     int
		wantedWidth int
	}{
		"an ordinary window":    {120, 45, 51, 50},
		"a window with no room": {80, 45, 51, 30},
		"a very wide window":    {400, 45, 51, 50},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			col, width := statusColumn(tc.termW, tc.qrW)
			if col != tc.wantCol {
				t.Errorf("column = %d, want %d", col, tc.wantCol)
			}
			if width != tc.wantedWidth {
				t.Errorf("width = %d, want %d", width, tc.wantedWidth)
			}
		})
	}
}

// Every address gets a code, and every code carries that address — the phone
// scans one of these and is pointed at whatever is inside it.
func TestRenderQRCodesBuildsOnePerAddress(t *testing.T) {
	urls := []string{"http://192.168.1.10:8080", "https://198.51.100.4:9443"}

	codes := renderQRCodes(urls, testRawKey, "")

	if len(codes) != len(urls) {
		t.Fatalf("rendered %d codes for %d addresses", len(codes), len(urls))
	}
	for i, lines := range codes {
		if len(lines) == 0 {
			t.Errorf("no code for %s", urls[i])
		}
	}
}

// An address too long to fit in a QR code leaves a nil entry rather than a
// missing one, so `qr <n>` still counts the same list the user was shown.
func TestRenderQRCodesKeepsTheIndexesLinedUpWhenOneWillNotRender(t *testing.T) {
	huge := "http://" + strings.Repeat("a", 4000) + ":8080"
	urls := []string{"http://192.168.1.10:8080", huge}

	codes := renderQRCodes(urls, testRawKey, "")

	if len(codes) != 2 {
		t.Fatalf("rendered %d codes for 2 addresses — the indexes no longer match", len(codes))
	}
	if len(codes[0]) == 0 {
		t.Error("the address that could render has no code")
	}
	if codes[1] != nil {
		t.Error("an address that cannot render was given a code anyway")
	}
}

// The two columns are centered against each other, whichever is shorter. In
// practice the QR code is the taller one, but the layout must not assume it:
// a laptop with every sensor listed and a small code would otherwise draw the
// code hard against the top with a column of blank beneath it.
func TestTheShorterColumnIsCentredAgainstTheTaller(t *testing.T) {
	var screen syncBuffer
	sb := &statusBar{
		out:       &screen,
		hub:       testHub(t),
		sensorMgr: monitor.NewManager(),
		gridWidth: 50,
		// One row of code against a status grid several rows tall.
		qrBoxW:  4,
		qrBoxH:  1,
		qrCodes: [][]string{{"####"}},
	}

	const startRow = 11
	rows := drawCodeAndStatus(&screen, sb, startRow, 51)

	statusH := len(sb.gridLines())
	if rows != statusH {
		t.Fatalf("the block is %d rows tall, want %d — the taller of the two", rows, statusH)
	}
	if sb.qrRow != startRow+(statusH-1)/2 {
		t.Errorf("the code starts on row %d, want it centered at %d", sb.qrRow, startRow+(statusH-1)/2)
	}
	// The taller column is the one that sets the height, so it is not moved.
	if sb.gridRow != startRow {
		t.Errorf("the status grid starts on row %d, want %d", sb.gridRow, startRow)
	}
}

// The header is drawn from row 1 down, and everything below it is positioned by
// the row this returns. Getting it wrong puts the QR code over the banner.
func TestHeaderReportsTheRowItFinishedOn(t *testing.T) {
	var screen syncBuffer

	row := drawHeader(&screen, "  ----")

	if row != 10 {
		t.Errorf("drawHeader returned row %d, want 10", row)
	}
	if lines := strings.Count(screen.String(), "\n"); lines != row-1 {
		t.Errorf("drew %d lines but claims to have finished on row %d", lines, row)
	}
}

func TestFooterReportsTheRowItFinishedOn(t *testing.T) {
	var screen syncBuffer

	row := drawFooter(&screen, "  ----", 30)

	if row != 33 {
		t.Errorf("drawFooter returned row %d, want 33", row)
	}
	if !strings.Contains(screen.String(), "Commands:") {
		t.Errorf("the footer did not list the commands; screen was:\n%s", screen.String())
	}
}

// itoa keeps the row assertions readable without pulling strconv into the file
// for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []rune
	for n > 0 {
		digits = append([]rune{rune('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// A QR code is square, so the box the layout reserves has to be as wide as it is
// tall in characters — the code is drawn two characters per module.
func TestQRCodeIsWiderThanItIsTall(t *testing.T) {
	codes := renderQRCodes([]string{"http://192.168.1.10:8080"}, testRawKey, "")
	if len(codes) != 1 || len(codes[0]) == 0 {
		t.Fatal("no code was rendered")
	}

	w, h := qrBoxSize(codes)
	if w <= h {
		t.Errorf("a code %d wide and %d tall is not drawn two characters per module", w, h)
	}
	if got := utf8.RuneCountInString(codes[0][0]); got != w {
		t.Errorf("the box is %d wide but the first line is %d", w, got)
	}
}
