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

	// The screen is a package-level singleton, because the paths that have to
	// give the terminal back do not all have an app to ask. One dashboard per
	// process is true of the program and not of a test file, so each build
	// hands it back before the next one takes it.
	t.Cleanup(terminalScreen.restore)
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

	l := sb.layout
	bottom := max(l.qrRow+l.qrBoxH, l.gridRow+len(sb.gridLines()))
	if l.logRow < bottom {
		t.Errorf("the log scrolls from row %d, over a dashboard that reaches row %d",
			l.logRow, bottom-1)
	}
	if !strings.Contains(drawn, "\033["+itoa(l.logRow)+";"+itoa(l.termH)+"r") {
		t.Errorf("no scrolling region was pinned at row %d", l.logRow)
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

// The status grid sits to the right of the QR box, and its width is clamped at
// both ends: too narrow and its lines wrap into nonsense, too wide and it
// sprawls across a maximized window with a yard of space between the columns.
//
// The window with no room is the one that mattered. The old arithmetic put the
// grid at column 51 with a width of 30 in an eighty-column window — nine columns
// past the right edge — so every line of it wrapped, pushing the rows below it
// down while the next repaint drew them back where they were. That is what two
// status grids on one screen looked like.
func TestTheGridIsPlacedBesideTheCodeAndKeptInsideTheWindow(t *testing.T) {
	cases := map[string]struct {
		termW, termH   int
		wantCol        int
		wantWidth      int
		wantSideBySide bool
	}{
		"an ordinary window":   {120, 40, 51, 50, true},
		"a very wide window":   {400, 40, 51, 50, true},
		"no room for the grid": {80, 40, 3, 50, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			l := computeLayout(tc.termW, tc.termH, 45, 23, 12)

			if l.gridCol != tc.wantCol {
				t.Errorf("column = %d, want %d", l.gridCol, tc.wantCol)
			}
			if l.gridWidth != tc.wantWidth {
				t.Errorf("width = %d, want %d", l.gridWidth, tc.wantWidth)
			}
			if right := l.gridCol + l.gridWidth - 1; right > tc.termW {
				t.Errorf("the grid ends at column %d, past the %d-column window", right, tc.termW)
			}
			if beside := l.gridRow < l.qrRow+l.qrBoxH && l.gridCol > l.qrCol; beside != tc.wantSideBySide {
				t.Errorf("side by side = %v, want %v", beside, tc.wantSideBySide)
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
	// One row of code against a status grid several rows tall.
	l := computeLayout(120, 40, 4, 1, 13)

	if l.gridRow != l.labelRow+1 {
		t.Errorf("the taller column starts on row %d, want %d — it sets the height",
			l.gridRow, l.labelRow+1)
	}
	if want := l.gridRow + (13-1)/2; l.qrRow != want {
		t.Errorf("the code starts on row %d, want it centered at %d", l.qrRow, want)
	}
}

// The banner is six rows of block letters that do not wrap gracefully: in a
// window too narrow for them each row folds onto the next, the header becomes
// twelve rows instead of six, and everything the layout placed below it lands on
// top of something else. So a narrow window gets the name on one line.
func TestTheBannerGivesWayInAWindowTooNarrowForIt(t *testing.T) {
	var wide, narrow, short syncBuffer

	drawHeader(&wide, computeLayout(120, 60, 45, 23, 12))
	drawHeader(&narrow, computeLayout(bannerWidth-1, 60, 45, 23, 12))
	// Wide enough for the block letters, and short enough that keeping them
	// would cost the code instead.
	drawHeader(&short, computeLayout(120, 32, 53, 18, 12))

	if !strings.Contains(wide.String(), "█") {
		t.Error("a wide window did not get the block letters")
	}
	if strings.Contains(narrow.String(), "█") {
		t.Error("a narrow window got block letters it has no room for")
	}
	// And the other reason they give way: a window wide enough for them but too
	// short for them and the code together. The header cannot know that on its
	// own, and one that drew six rows where the layout allowed two put its own
	// banner underneath everything placed below it.
	if strings.Contains(short.String(), "█") {
		t.Error("a short window kept the block letters and gave up the code instead")
	}
	for _, drawn := range []string{wide.String(), narrow.String(), short.String()} {
		if !strings.Contains(drawn, "Device Security Monitor") {
			t.Error("the header does not say what the program is")
		}
	}
	if got := bannerHeight(bannerWidth - 1); got != shortBannerRows {
		t.Errorf("a narrow header is %d rows, want %d", got, shortBannerRows)
	}
}

// And nothing the header draws may reach the row the label above the code goes
// on. That overlap is what put "Scan to connect:" through the middle of the
// banner.
func TestTheHeaderStopsAboveTheLabel(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 60}, {120, 30}, {80, 24}, {60, 40}} {
		var screen syncBuffer
		l := computeLayout(size.w, size.h, 53, 15, 12)

		drawHeader(&screen, l)

		for row := l.labelRow; row <= size.h; row++ {
			if strings.Contains(screen.String(), "\x1b["+itoa(row)+";1H") {
				t.Errorf("%dx%d: the header drew on row %d, at or below the label at %d",
					size.w, size.h, row, l.labelRow)
			}
		}
	}
}

func TestTheFooterSitsJustAboveTheLog(t *testing.T) {
	var screen syncBuffer
	l := computeLayout(120, 40, 45, 23, 12)

	drawFooter(&screen, l)

	drawn := screen.String()
	if !strings.Contains(drawn, "Commands:") {
		t.Errorf("the footer did not list the commands; screen was:\n%s", drawn)
	}
	if !strings.Contains(drawn, "\x1b["+itoa(l.logRow-2)+";1H") {
		t.Errorf("the command list is not two rows above the log at %d", l.logRow)
	}
}

// A window narrower than the command list is not a reason to wrap it. A wrapped
// footer pushes the first log line into the scrolling region the layout just
// pinned, and the screen ends up a row out of step with what was drawn.
func TestTheFooterIsCutRatherThanWrapped(t *testing.T) {
	var screen syncBuffer
	l := computeLayout(60, 40, 45, 23, 12)

	drawFooter(&screen, l)

	for _, line := range strings.Split(screen.String(), "\x1b[") {
		if i := strings.Index(line, "H"); i >= 0 {
			if got := visLen(line[i+1:]); got > l.termW {
				t.Errorf("a footer row is %d columns wide in a %d-column window: %q",
					got, l.termW, line[i+1:])
			}
		}
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
