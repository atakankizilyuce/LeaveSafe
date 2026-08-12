package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/remote"
)

// The dashboard is a full-screen program drawn on a terminal somebody else was
// already using. Everything below is about the borrowing being visible at both
// ends: taken on the alternate screen, given back whole.

func TestTheDashboardDrawsOnTheAlternateScreen(t *testing.T) {
	_, drawn := drawnDashboard(t, remote.State{})

	if !strings.HasPrefix(drawn, altScreenOn) {
		t.Errorf("the dashboard did not switch to the alternate screen first; it began with %q",
			firstEscape(drawn))
	}
}

// Restoring is what the user gets back: a window that scrolls all the way, their
// own screen with their own scrollback, a cursor they can see, and no color
// left set from the last thing drawn.
func TestRestoreGivesTheWholeTerminalBack(t *testing.T) {
	var out syncBuffer
	s := &screen{}
	s.enter(&out)

	s.restore()

	given := strings.TrimPrefix(out.String(), altScreenOn)
	for _, want := range []struct{ what, seq string }{
		{"the scrolling region was not reset", scrollWhole},
		{"the user's own screen was not restored", altScreenOff},
		{"the cursor was left hidden", cursorShow},
		{"a color was left set", cReset},
	} {
		if !strings.Contains(given, want.seq) {
			t.Errorf("%s: restore wrote %q", want.what, given)
		}
	}
}

// Every way out of the program calls this, and they overlap: a Ctrl+C runs
// shutdown and then the deferred close, and a log.Fatal reaches it through
// logrus's exit handler instead. A second call must not write escapes into a
// terminal the shell has already taken back.
func TestRestoreIsIdempotent(t *testing.T) {
	var out syncBuffer
	s := &screen{}
	s.enter(&out)
	s.restore()
	before := out.String()

	s.restore()
	s.restore()

	if got := out.String(); got != before {
		t.Errorf("restoring twice wrote more: %q", strings.TrimPrefix(got, before))
	}
}

// A start that failed before drawing anything, or a -plain run, never took the
// terminal. Writing the sequences anyway would clear a screen the program does
// not own — on a headless start, into a log file.
func TestRestoreOfATerminalNeverTakenWritesNothing(t *testing.T) {
	s := &screen{}

	s.restore()

	if s.active() {
		t.Error("a screen that was never entered reports itself active")
	}
}

// Entering twice would clear the alternate screen a second time, taking the
// dashboard already drawn on it with it.
func TestEnteringTwiceDrawsOnce(t *testing.T) {
	var out syncBuffer
	s := &screen{}

	s.enter(&out)
	s.enter(&out)

	if got := strings.Count(out.String(), altScreenOn); got != 1 {
		t.Errorf("switched to the alternate screen %d times, want 1", got)
	}
}

// A repaint is the whole dashboard again, not a touch-up. Coming back from
// Ctrl+Z lands on an alternate screen the terminal cleared on the way in, so
// anything paint leaves out is gone until the program is restarted.
func TestPaintRedrawsEverythingTheFirstDrawPutOnScreen(t *testing.T) {
	sb, first := drawnDashboard(t, remote.State{})

	var again syncBuffer
	sb.out = &again
	sb.paint()

	for _, part := range []string{"Device Security Monitor", "Scan to connect:", "Ctrl+C to quit"} {
		if !strings.Contains(again.String(), part) {
			t.Errorf("a repaint left out %q", part)
		}
	}
	if !strings.Contains(again.String(), "\033[2J") {
		t.Error("a repaint did not clear the screen it was drawing over")
	}
	if strings.Contains(first, "STATUS") && !strings.Contains(again.String(), "STATUS") {
		t.Error("a repaint left out the status grid")
	}
}

// Redirected to a file, the dashboard's cursor escapes are not decoration that
// can be ignored — they are the file's contents. The program has to know it has
// nowhere to draw before it starts drawing.
func TestAFileIsNotSomethingToDrawOn(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "not-a-terminal"))
	if err != nil {
		t.Fatalf("create the file: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	if drawableTerminal(f) {
		t.Error("a plain file was taken for a terminal")
	}
	if drawableTerminal(nil) {
		t.Error("nothing at all was taken for a terminal")
	}
}

// How a run decides what to put on screen. Getting this wrong in the direction
// of "draw it anyway" is what wrote cursor escapes into a redirected log file.
func TestHowARunDecidesWhatToPutOnScreen(t *testing.T) {
	notATerminal, err := os.Create(filepath.Join(t.TempDir(), "log.txt"))
	if err != nil {
		t.Fatalf("create the file: %v", err)
	}
	t.Cleanup(func() { _ = notATerminal.Close() })

	cases := map[string]struct {
		headless, plain      bool
		out                  *os.File
		wantPlain, wantDrawn bool
	}{
		"redirected to a file": {out: notATerminal, wantPlain: true},
		"-plain asked for":     {plain: true, out: notATerminal, wantPlain: true},
		"headless":             {headless: true, out: notATerminal},
		"headless outranks -plain": {
			headless: true, plain: true, out: notATerminal,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			plain, drawn := planTerminal(tc.headless, tc.plain, tc.out)
			if plain != tc.wantPlain {
				t.Errorf("plain = %v, want %v", plain, tc.wantPlain)
			}
			if drawn != tc.wantDrawn {
				t.Errorf("dashboard = %v, want %v", drawn, tc.wantDrawn)
			}
		})
	}
}

// A headless status bar draws nothing at all. It is handed io.Discard, but the
// check has to be in the drawing rather than in where it lands: the escapes are
// positioned against a terminal, and a headless run's output is a log file.
func TestAHeadlessStatusBarDrawsNothing(t *testing.T) {
	var out syncBuffer
	sb := newHeadlessStatusBar(testHub(t), monitor.NewManager(), "key", testRawKey, nil, "", "")
	sb.out = &out

	sb.paint()

	if written := out.String(); written != "" {
		t.Errorf("a headless run drew a dashboard: %q", written)
	}
}

// A window too small is treated the same as no answer at all: the layout
// assumes it has room, and squeezing it into eighty by twenty produces a QR
// code cut in half, which is worse than one running off the edge of a window
// the user can resize.
func TestTheSizeTheDashboardIsLaidOutFor(t *testing.T) {
	cases := map[string]struct {
		w, h         int
		err          error
		wantW, wantH int
	}{
		"a window that answers": {w: 200, h: 60, wantW: 200, wantH: 60},
		"a window too narrow":   {w: 70, h: 60, wantW: 120, wantH: 40},
		"a window too short":    {w: 200, h: 12, wantW: 120, wantH: 40},
		"nothing to ask":        {err: errNoSize, wantW: 120, wantH: 40},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			real := termSizeFn
			termSizeFn = func(int) (int, int, error) { return tc.w, tc.h, tc.err }
			t.Cleanup(func() { termSizeFn = real })

			w, h := terminalSize(os.Stdout)

			if w != tc.wantW || h != tc.wantH {
				t.Errorf("terminalSize = %dx%d, want %dx%d", w, h, tc.wantW, tc.wantH)
			}
		})
	}

	// Nothing at all to ask, which is what a headless start and every test that
	// draws into a buffer hands it.
	if w, h := terminalSize(nil); w != 120 || h != 40 {
		t.Errorf("terminalSize(nil) = %dx%d, want the 120x40 fallback", w, h)
	}
}

// errNoSize stands in for whatever the platform says when the thing it was
// handed is not a terminal.
var errNoSize = errors.New("not a terminal")

// -plain has no dashboard, so this is the only place a code to scan appears.
// Without it the user is left with a URL to type on a phone keyboard and a
// pairing key to copy by eye.
func TestPlainOutputStillPrintsSomethingToScan(t *testing.T) {
	var out syncBuffer
	sb := newHeadlessStatusBar(testHub(t), monitor.NewManager(), "1111 1111 1111 1116",
		testRawKey, []string{"http://192.168.1.10:8080"}, "", "")

	printPairingCode(&out, sb, testRawKey, "")

	printed := out.String()
	if !strings.Contains(printed, "█") && !strings.Contains(printed, "▀") {
		t.Errorf("no QR code was printed; output was:\n%s", printed)
	}
	if !strings.Contains(printed, "http://192.168.1.10:8080") {
		t.Errorf("the address to open was not printed; output was:\n%s", printed)
	}
	if !strings.Contains(printed, "1111 1111 1111 1116") {
		t.Errorf("the pairing key was not printed; output was:\n%s", printed)
	}
	// Positioning escapes belong to a dashboard. This output scrolls.
	if strings.Contains(printed, "\033[2J") {
		t.Error("the plain output cleared the screen")
	}
}

// An address too long to fit in a QR code has to be said out loud. Printing
// nothing would leave the user staring at a blank space where a code should be,
// with no idea whether to wait or type the address in.
func TestPlainOutputSaysWhenThereIsNoCodeToPrint(t *testing.T) {
	var out syncBuffer
	huge := "http://" + strings.Repeat("a", 4000) + ":8080"
	sb := newHeadlessStatusBar(testHub(t), monitor.NewManager(), "key", testRawKey,
		[]string{huge}, "", "")

	printPairingCode(&out, sb, testRawKey, "")

	if !strings.Contains(out.String(), "No QR code") {
		t.Errorf("nothing said why there was no code; output was:\n%s", out.String())
	}
}

// A start with nowhere to connect to has nothing to print, and must not print a
// heading promising a code that never follows.
func TestPlainOutputPrintsNothingWithNoAddress(t *testing.T) {
	var out syncBuffer
	sb := newHeadlessStatusBar(testHub(t), monitor.NewManager(), "key", testRawKey, nil, "", "")

	printPairingCode(&out, sb, testRawKey, "")

	if written := out.String(); written != "" {
		t.Errorf("something was printed for a run with no address: %q", written)
	}
}

// firstEscape names what a string starts with, for a failure message that says
// which escape arrived instead of the expected one.
func firstEscape(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
