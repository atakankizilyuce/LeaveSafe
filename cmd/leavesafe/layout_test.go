package main

import (
	"os"
	"strings"
	"testing"

	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/remote"
)

// A QR code for an address with a pairing key in it is about twenty-eight rows
// tall, and an ordinary terminal window is thirty. Everything below is about
// what gives way, and in what order, when they will not both fit.

// The rule the whole file rests on: nothing drawn once may occupy a row the log
// scrolls through. Break it and every log line scrolls part of the dashboard
// away, while the next repaint draws that part back where it was — which is what
// two status grids on one screen were.
func TestNothingDrawnOnceReachesIntoTheLog(t *testing.T) {
	sizes := []struct{ w, h int }{
		{200, 60}, {120, 40}, {120, 30}, {100, 30}, {80, 24}, {60, 20}, {40, 15}, {20, 10},
	}
	for _, qr := range []struct{ w, h int }{{0, 0}, {21, 11}, {53, 27}, {77, 39}} {
		for _, size := range sizes {
			l := computeLayout(size.w, size.h, qr.w, qr.h, 12)

			bottom := max(l.qrRow+l.qrBoxH, l.gridRow+l.gridRows)
			if l.logRow < bottom {
				t.Errorf("%dx%d with a %dx%d code: the log starts at row %d, "+
					"over a dashboard that reaches row %d",
					size.w, size.h, qr.w, qr.h, l.logRow, bottom-1)
			}
			if l.logRow > size.h {
				t.Errorf("%dx%d with a %dx%d code: the log starts at row %d, off the bottom",
					size.w, size.h, qr.w, qr.h, l.logRow)
			}
			if right := l.gridCol + l.gridWidth - 1; right > size.w && size.w >= floorGridWidth+2 {
				t.Errorf("%dx%d with a %dx%d code: the grid ends at column %d, past the edge",
					size.w, size.h, qr.w, qr.h, right)
			}
		}
	}
}

// The order things give way in. A window too short for everything drops the
// block letters before it drops the code, because the letters are decoration
// and the code is what the program is for.
func TestTheBannerGoesBeforeTheCodeDoes(t *testing.T) {
	// Tall enough for the code with a short header, not with the full one.
	const qrH = 27
	l := computeLayout(120, 38, 53, qrH, 12)

	if l.qrBoxH != qrH {
		t.Fatalf("the code was dropped (box height %d) in a window that had room for it "+
			"without the banner", l.qrBoxH)
	}
	if l.labelRow != shortBannerRows+2 {
		t.Errorf("the label is at row %d, want %d — the banner did not give way",
			l.labelRow, shortBannerRows+2)
	}
}

// A window too narrow for both columns puts the code above the grid rather than
// drawing one over the other.
func TestANarrowWindowStacksTheCodeAboveTheGrid(t *testing.T) {
	l := computeLayout(60, 60, 53, 27, 12)

	if l.qrBoxH == 0 {
		t.Fatal("the code was dropped by a window tall enough to stack it")
	}
	if l.gridRow < l.qrRow+l.qrBoxH {
		t.Errorf("the grid starts at row %d, inside a code that runs to row %d",
			l.gridRow, l.qrRow+l.qrBoxH-1)
	}
	if l.gridCol != l.qrCol {
		t.Errorf("stacked, both start at the left margin; the grid is at column %d and the code at %d",
			l.gridCol, l.qrCol)
	}
}

// And when neither shape fits, the code goes. It is the one part that cannot be
// made smaller — it is as many modules as the address it carries.
func TestAWindowWithNoRoomGetsNoCode(t *testing.T) {
	l := computeLayout(40, 16, 53, 27, 12)

	if l.qrBoxH != 0 {
		t.Errorf("a %dx%d window was given a 53x27 code", l.termW, l.termH)
	}
	if l.gridRow == 0 || l.gridWidth == 0 {
		t.Error("the addresses were left with nowhere to be listed either")
	}
}

// The footer is drawn at fixed rows above the log, so a grid that reached them
// was drawn over: an address to pair with, with a list of commands through the
// middle of it. In a window with room for one of the two, the grid is the one
// worth keeping — `help` says everything the footer says, and nothing says the
// address.
func TestASmallWindowKeepsTheAddressesAndDropsTheCommandList(t *testing.T) {
	// A window of the size this actually bites at: 80 columns, and 23 rows
	// once the input line has its own.
	const gridH = 10
	l := computeLayout(80, 23, 53, 27, gridH)

	if l.footerShown {
		t.Fatal("the command list was kept in a window with no room for it and the grid")
	}
	if l.gridRows != gridH {
		t.Errorf("%d of the grid's %d rows are drawn; the addresses were clipped to keep a footer that is gone",
			l.gridRows, gridH)
	}
	if l.gridRow+l.gridRows > l.logRow {
		t.Errorf("the grid runs to row %d, into a log that starts at %d",
			l.gridRow+l.gridRows-1, l.logRow)
	}
}

// And it comes back the moment there is room, because it is how somebody who
// has never run this before finds out there is anything to type.
func TestAWindowWithRoomKeepsTheCommandList(t *testing.T) {
	l := computeLayout(80, 30, 53, 27, 10)

	if !l.footerShown {
		t.Error("the command list was dropped by a window that had room for it")
	}
}

// Nothing is drawn where the footer would have been, or the grid would be
// painted over by the very thing that made room for it.
func TestNoFooterIsDrawnWhenThereIsNoRoomForOne(t *testing.T) {
	var out syncBuffer

	drawFooter(&out, computeLayout(80, 23, 53, 27, 10))

	if out.String() != "" {
		t.Errorf("a footer was drawn into a window with no room for one: %q", out.String())
	}
}

// The label above the code says which of those happened, because a user looking
// for something to scan should not have to work out that there is nothing there.
func TestTheLabelSaysWhenThereIsNoCode(t *testing.T) {
	var withCode, without syncBuffer

	drawScanLabel(&withCode, computeLayout(120, 60, 53, 27, 12))
	drawScanLabel(&without, computeLayout(40, 16, 53, 27, 12))

	if !strings.Contains(withCode.String(), "Scan to connect") {
		t.Errorf("nothing invited the user to scan; label was %q", withCode.String())
	}
	if !strings.Contains(without.String(), "No room for a QR code") {
		t.Errorf("nothing said why there was no code; label was %q", without.String())
	}
}

// The grid's height is answered by counting rather than by drawing, because the
// layout asks before every repaint and drawing means asking each sensor whether
// it is available. The two must not drift.
func TestTheCountedGridHeightMatchesTheDrawnOne(t *testing.T) {
	cases := map[string]*statusBar{
		"the least there can be": {urls: nil},
		"one address":            {urls: []string{"http://192.168.1.10:8080"}},
		"remote access on": {
			urls:         []string{"http://192.168.1.10:8080", "https://198.51.100.4:9443"},
			remoteStatus: "ACTIVE — 198.51.100.4:9443",
			certFP:       "AA:BB:CC:DD",
		},
	}
	for name, sb := range cases {
		t.Run(name, func(t *testing.T) {
			sb.hub = testHub(t)
			sb.sensorMgr = monitor.NewManager()
			sb.out = &syncBuffer{}

			if counted, drawn := sb.gridHeight(), len(sb.gridLines()); counted != drawn {
				t.Errorf("the layout is told the grid is %d rows; it draws %d", counted, drawn)
			}
		})
	}
}

// A line wider than the box it goes in wraps, and a wrapped line pushes every
// row below it down while the layout still believes they are where it put them.
// The remote-access line is the one that outgrows a narrow box without warning.
func TestNoGridLineIsWiderThanTheBox(t *testing.T) {
	sb := &statusBar{
		hub:          testHub(t),
		sensorMgr:    monitor.NewManager(),
		out:          &syncBuffer{},
		key:          "1111-1111-1111-1116",
		urls:         []string{"http://" + strings.Repeat("x", 90) + ":8080"},
		remoteStatus: "ON — not reachable yet, forward TCP 9443 by hand from the router's admin page",
		certFP:       "AA:BB:CC:DD:EE:FF:00:11:22:33",
		layout:       computeLayout(80, 40, 0, 0, 12),
	}

	for i, line := range sb.gridLines() {
		if got := visLen(line); got != sb.layout.gridWidth {
			t.Errorf("grid line %d is %d columns wide, want exactly %d: %q",
				i, got, sb.layout.gridWidth, line)
		}
	}
}

// Color escapes are zero-width, so a cut that counts them takes visible
// characters off a line that had room for them — and one that ignores the cut
// entirely leaves the rest of the row painted in the last color set.
func TestCuttingALineLeavesTheColorsClosed(t *testing.T) {
	cases := map[string]struct {
		in   string
		max  int
		want string
	}{
		"nothing to cut":     {"plain", 10, "plain"},
		"cut to the bone":    {"plain", 0, ""},
		"escapes are free":   {cGreen + "abc" + cReset, 3, cGreen + "abc" + cReset},
		"color is closed":    {cGreen + "abcdef", 3, cGreen + "abc" + cReset},
		"a negative is zero": {"abc", -1, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := truncateVisible(tc.in, tc.max)

			if got != tc.want {
				t.Errorf("truncateVisible(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
			if visLen(got) > max(tc.max, 0) {
				t.Errorf("%q is %d visible characters, want at most %d", got, visLen(got), tc.max)
			}
		})
	}
}

// An address shortened to fit says that it was, because one silently missing
// its last few characters is one the user copies down wrong and finds out about
// on a phone that will not connect.
func TestAShortenedAddressSaysItWasShortened(t *testing.T) {
	cases := map[string]struct {
		in   string
		max  int
		want string
	}{
		"it fits":               {"http://a:80", 20, "http://a:80"},
		"exactly":               {"abcde", 5, "abcde"},
		"cut, and marked as so": {"http://192.168.1.10:8080", 12, "http://19..."},
		"no room to mark it":    {"abcdef", 2, ".."},
		"no room at all":        {"abcdef", 0, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := ellipsize(tc.in, tc.max)

			if got != tc.want {
				t.Errorf("ellipsize(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
			if visLen(got) > max(tc.max, 0) {
				t.Errorf("%q is %d visible characters, want at most %d", got, visLen(got), tc.max)
			}
		})
	}
}

// A window with room for part of the grid gets part of it. The rest is not
// drawn into the rows the log scrolls: it would not be shown there either, it
// would scroll away a line at a time while the layout went on believing it was
// on screen.
func TestAGridTooTallForTheWindowIsClippedRatherThanScrolled(t *testing.T) {
	var screen syncBuffer
	sb := &statusBar{
		hub:       testHub(t),
		sensorMgr: monitor.NewManager(),
		out:       &screen,
		key:       "1111-1111-1111-1116",
		urls:      []string{"http://192.168.1.10:8080"},
	}
	sb.layout = computeLayout(60, 16, 0, 0, sb.gridHeight())
	if sb.layout.gridRows >= sb.gridHeight() {
		t.Fatalf("a %d-row window had room for the whole %d-row grid; nothing to clip",
			sb.layout.termH, sb.gridHeight())
	}

	sb.drawGrid()

	for row := sb.layout.logRow; row <= sb.layout.termH; row++ {
		if strings.Contains(screen.String(), "\033["+itoa(row)+";") {
			t.Errorf("the grid was drawn on row %d, which the log scrolls", row)
		}
	}
}

// The same for a code taller than the box reserved for it. Drawing past the box
// puts QR modules over whatever the layout placed below.
func TestACodeTallerThanItsBoxIsClipped(t *testing.T) {
	var screen syncBuffer
	sb := &statusBar{
		out:      &screen,
		qrCodes:  [][]string{{"##", "##", "##", "##"}},
		qrURLIdx: 0,
		layout:   layout{qrRow: 5, qrCol: 3, qrBoxW: 2, qrBoxH: 2},
	}

	sb.drawQR()

	for _, row := range []int{7, 8} {
		if strings.Contains(screen.String(), "\033["+itoa(row)+";3H##") {
			t.Errorf("the code was drawn on row %d, past the two rows reserved for it", row)
		}
	}
}

// A headless run has no screen. The check has to be in the drawing rather than
// in where it lands, because its output is a log file and cursor escapes in one
// are garbage.
func TestAHeadlessRunDrawsNoGrid(t *testing.T) {
	var screen syncBuffer
	sb := newHeadlessStatusBar(testHub(t), monitor.NewManager(), "key", testRawKey, nil, "", "")
	sb.out = &screen

	sb.drawGrid()

	if written := screen.String(); written != "" {
		t.Errorf("a headless run drew a status grid: %q", written)
	}
}

// Rotating the key invalidates every code on screen. They have to be rebuilt and
// redrawn, or the dashboard keeps offering the key the user just revoked.
func TestRotatingTheKeyRedrawsTheCode(t *testing.T) {
	sb, _ := drawnDashboard(t, remote.State{})
	before := append([][]string(nil), sb.qrCodes...)

	sb.rekeyQR("2222222222222224")

	if len(sb.qrCodes) == 0 {
		t.Fatal("there were no codes to rotate")
	}
	same := true
	for i := range sb.qrCodes {
		if len(before[i]) != len(sb.qrCodes[i]) {
			same = false
			break
		}
		for j := range sb.qrCodes[i] {
			if before[i][j] != sb.qrCodes[i][j] {
				same = false
				break
			}
		}
	}
	if same {
		t.Error("the codes still encode the key that was just invalidated")
	}
}

// The check that stops a repaint drawing a second copy of anything. Remote
// access arriving a minute after startup adds an address, the grid grows a row,
// and every row below it moves — so painting the grid alone at the rows it used
// to occupy is what has to be caught.
func TestARepaintNoticesWhenTheLayoutHasMoved(t *testing.T) {
	sb, _ := drawnDashboard(t, remote.State{})
	before := sb.layout

	sb.setURLs([]string{"http://192.168.1.10:8080", "https://198.51.100.4:9443"})

	if sb.layout == before {
		t.Fatal("an extra address changed nothing about the layout")
	}
	if !sb.layoutHolds() {
		t.Error("the dashboard was left believing a layout the screen does not have")
	}
}

// A window that changed size moves every absolute row the dashboard drew at.
// Nothing signals this on Windows, so the five-second repaint has to notice it
// on its own.
func TestARepaintNoticesTheWindowChangingSize(t *testing.T) {
	sb, _ := drawnDashboard(t, remote.State{})

	real := termSizeFn
	t.Cleanup(func() { termSizeFn = real })

	// drawnDashboard draws into a file, which cannot be asked its size and gets
	// the 120x40 fallback. This is the same run in a window half as tall.
	sb.term = os.Stdout
	termSizeFn = func(int) (int, int, error) { return 100, 20, nil }

	if sb.layoutHolds() {
		t.Fatal("the window halved and the dashboard did not notice")
	}

	sb.paint()

	if !sb.layoutHolds() {
		t.Error("the dashboard was redrawn and still does not match the window")
	}
	if sb.layout.termH != 20 {
		t.Errorf("redrawn for a %d-row window, want 20", sb.layout.termH)
	}
}

// The order everything gives way in, with the code at the end of it. A window
// that has room for the code once the decoration is gone must be given the
// code: it is the one part of this screen that is there to be used rather than
// read, and the addresses under it are useless to a phone that cannot be
// pointed at them.
func TestTheDecorationGoesBeforeTheCodeDoes(t *testing.T) {
	// A local code and a default terminal window, one row of which the input
	// line has taken. Everything fits here only if something gives way.
	l := computeLayout(120, 29, 37, 19, 12)

	if l.qrBoxH != 19 {
		t.Fatalf("the code was dropped (box height %d) in a window with room for it "+
			"once the banner and the command list gave way", l.qrBoxH)
	}
	if l.headRows != shortBannerRows {
		t.Errorf("the banner is %d rows; it should have gone before the code was at risk", l.headRows)
	}
	if l.footerShown {
		t.Error("the command list was kept in a window that had to choose between it and the code")
	}
	if l.logRow+minLogRows-1 > l.termH {
		t.Errorf("the log starts at row %d and needs %d rows in a window of %d",
			l.logRow, minLogRows, l.termH)
	}
}

// And it comes back the moment the window can hold both, because a screen that
// permanently hides how to quit is its own bug.
func TestARoomyWindowKeepsTheCodeAndTheCommandList(t *testing.T) {
	l := computeLayout(120, 40, 37, 19, 12)

	if l.qrBoxH != 19 {
		t.Errorf("the code was dropped by a window with room for everything")
	}
	if !l.footerShown {
		t.Error("the command list was dropped by a window with room for everything")
	}
}
