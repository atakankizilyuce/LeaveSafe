package main

import (
	"fmt"
	"io"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"
	"golang.org/x/term"
)

// The escapes that take a terminal over and the ones that give it back.
//
// altScreenOn is the important one. The dashboard draws at absolute positions,
// clears the screen and pins a scrolling region under its header — on the
// terminal the user was already using, that wipes out their scrollback and
// leaves them with a window they cannot scroll. The alternate screen buffer is
// the terminal's own answer to this: a second, empty screen that full-screen
// programs draw on, with the first one held untouched underneath and handed
// back on the way out.
const (
	altScreenOn  = "\033[?1049h"
	altScreenOff = "\033[?1049l"
	// scrollWhole resets the scrolling region (DECSTBM) to the full window.
	// Without it a terminal is left able to scroll only the bottom few rows,
	// which is what the dashboard leaves behind and what the shell inherits.
	scrollWhole = "\033[r"
	cursorShow  = "\033[?25h"
	// The terminal's own one-slot cursor store. With an input line on screen it
	// holds where the log left off, because the cursor itself is on the row the
	// user is typing on and the log has to be written somewhere else.
	cursorSave    = "\033[s"
	cursorRestore = "\033[u"
)

// screen is the terminal takeover, and the one thing that has to be undone
// however the program ends.
//
// It is a package-level value rather than something the app carries because the
// ways out do not all go through the app: a log.Fatal exits from inside logrus,
// a failed start never builds an app at all, and a signal arrives on a
// goroutine of its own. Every one of those has to be able to say "give the
// terminal back" without holding anything.
type screen struct {
	mu      sync.Mutex
	out     io.Writer
	entered bool

	// keys is the terminal typing arrives from, when this program is reading it
	// a keystroke at a time. Taking the screen then means taking the keyboard
	// too: raw mode is what stops the terminal echoing typed characters into
	// the middle of the log, and it has to be given back at exactly the same
	// moments the screen is.
	keys *os.File
	// cooked is how the keyboard was set before raw mode, and nil when it was
	// never taken.
	cooked *term.State

	// consoleBack undoes whatever this platform needed doing to the console
	// before a dashboard could be drawn on it at all, and is nil where nothing
	// was. See takeConsole — on Windows it is two settings, and everywhere else
	// there is nothing to ask for.
	consoleBack func()
}

// The two calls that take the keyboard and give it back, as variables so a test
// can stand in for them: `go test` is attached to a pipe, and every terminal
// there is to put into raw mode is the failure path rather than the working
// one. Nothing in the running program reassigns them.
var (
	makeRawFn    = term.MakeRaw
	restoreRawFn = term.Restore
)

// terminalScreen is the one the running program uses.
var terminalScreen = &screen{}

func init() {
	// Whatever happens, the terminal is handed back. log.Fatal exits from
	// inside logrus without running a single deferred function, and until this
	// was registered a failed start left the user on an alternate screen with a
	// scrolling region pinned across it and no program left to undo either.
	//
	// Registered here rather than in main because this is the thing being
	// undone. Nothing is taken over until enter is called, and restore does
	// nothing when nothing was taken.
	log.RegisterExitHandler(terminalScreen.restore)
}

// enter switches to the alternate screen buffer, and remembers where to write
// the sequence that switches back.
//
// Calling it twice is not an error and does not draw twice: the second call
// would clear a screen the dashboard had already drawn on.
func (s *screen) enter(out io.Writer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entered {
		return
	}
	s.out = out
	s.entered = true
	s.takeKeyboard()
	// Before the first escape is written, because on Windows whether an escape
	// means anything at all is a console setting rather than a given — and
	// after the keyboard, because the input mode this asks for is the one raw
	// mode has just installed. restore undoes the three in the other order.
	s.consoleBack = takeConsole(asFile(out), s.keys)
	fmt.Fprint(out, altScreenOn)
}

// asFile is out as the operating system's handle, or nil when it is not one.
//
// The dashboard is drawn through an io.Writer so that a test can read what it
// drew, and the console settings above are asked for on a handle. A buffer has
// no handle and nothing to ask.
func asFile(out io.Writer) *os.File {
	f, _ := out.(*os.File)
	return f
}

// readsKeys says which terminal the typing comes from, so that taking the
// screen takes the keyboard with it.
//
// Input that is not a terminal — a pipe, a file, a test — is not taken: there
// is nothing there to put into raw mode, and nothing to read a keystroke at a
// time from either. Called before enter, because enter is what acts on it.
func (s *screen) readsKeys(in *os.File) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !drawableTerminal(in) {
		return
	}
	s.keys = in
}

// takeKeyboard puts the terminal into raw mode, where a keystroke arrives as
// soon as it is pressed and nothing is echoed until this program echoes it.
//
// A terminal that refuses is not a reason to stop: the dashboard is still worth
// drawing, and what is lost is the input line, so the keyboard is dropped and
// the terminal goes on echoing typed characters the way it always did.
func (s *screen) takeKeyboard() {
	if s.keys == nil {
		return
	}
	state, err := makeRawFn(int(s.keys.Fd()))
	if err != nil {
		log.Debugf("Could not take the keyboard, so typed characters are echoed by the terminal: %v", err)
		s.keys = nil
		return
	}
	s.cooked = state
}

// giveBackKeyboard undoes takeKeyboard. Without it a terminal is left with no
// echo and no line editing at all — a shell that shows nothing of what is
// typed, which is a worse thing to leave behind than a pinned scrolling region.
func (s *screen) giveBackKeyboard() {
	if s.keys == nil || s.cooked == nil {
		return
	}
	if err := restoreRawFn(int(s.keys.Fd()), s.cooked); err != nil {
		log.Debugf("Could not give the keyboard back: %v", err)
	}
	s.cooked = nil
}

// takesKeystrokes reports whether the keyboard is this program's to read one
// keystroke at a time.
func (s *screen) takesKeystrokes() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys != nil && s.cooked != nil
}

// restore gives the terminal back: full scrolling region, the user's screen,
// a visible cursor and no color left set.
//
// Safe to call when nothing was ever taken and safe to call twice, because the
// paths that call it overlap by design. A Ctrl+C runs shutdown and then the
// deferred close; a start that failed runs only the close; a log.Fatal runs
// neither and reaches this through logrus's exit handler. Making it idempotent
// is cheaper than working out which one got here first.
//
// The scrolling region is reset on both screens. Terminals disagree about
// whether the margins belong to the screen buffer or to the terminal, and the
// cost of resetting the one that did not need it is nothing.
func (s *screen) restore() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.entered {
		return
	}
	s.entered = false
	// Undone in the reverse of the order enter took them, which is not a
	// tidiness: the console mode asked for there was read off the keyboard raw
	// mode had already installed, so putting the keyboard back first and the
	// console mode back second would reinstall raw mode on the way out — a
	// shell with no echo and no line editing, which is the thing this whole
	// file exists to stop. The escapes go first of all, while the console is
	// still set to act on them.
	fmt.Fprint(s.out, scrollWhole+altScreenOff+scrollWhole+cursorShow+cReset)
	if s.consoleBack != nil {
		s.consoleBack()
		s.consoleBack = nil
	}
	s.giveBackKeyboard()
	s.out = nil
}

// active reports whether the terminal is currently taken over. Suspending has
// to know, so that resuming does not take a terminal the program never had.
func (s *screen) active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entered
}

// drawableTerminal reports whether out is something the dashboard can be drawn
// on: a terminal, rather than a file or a pipe.
func drawableTerminal(out *os.File) bool {
	return out != nil && isTerminalFn(int(out.Fd()))
}

// isTerminalFn is term.IsTerminal, as a variable so a test can answer for a
// terminal no test process has: `go test` is attached to a pipe, and every
// decision that depends on there being one would otherwise only ever be tested
// in the direction where there is not. Nothing in the running program
// reassigns it.
var isTerminalFn = term.IsTerminal

// planTerminal settles how this run talks to the terminal: with a dashboard,
// with plain log lines, or with neither.
//
// A dashboard needs somewhere to draw. Redirected to a file or piped into
// something, the cursor escapes it is built from are not decoration that can be
// ignored — they are the file's contents, and the layout is positioned against
// a window size that was never asked for. Falling back to log lines is the
// honest answer there, and -plain is the same fallback asked for by hand.
//
// A headless start is neither: it has no terminal at all, and the fallback
// would be describing a screen nobody is looking at.
func planTerminal(headless, plain bool, out *os.File) (usePlain, drawDashboard bool) {
	if headless {
		return false, false
	}
	if !plain && !drawableTerminal(out) {
		log.Info("Standard output is not a terminal — running without the dashboard")
		plain = true
	}
	return plain, !plain
}
