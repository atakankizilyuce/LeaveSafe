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
}

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
	fmt.Fprint(out, altScreenOn)
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
	fmt.Fprint(s.out, scrollWhole+altScreenOff+scrollWhole+cursorShow+cReset)
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
	return out != nil && term.IsTerminal(int(out.Fd()))
}

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
