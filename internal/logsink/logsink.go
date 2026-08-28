// Package logsink puts a queue between the logger and whatever it writes to,
// so that writing a log line can never stop this program.
//
// It exists because of one afternoon on a real machine. The desktop
// application starts this daemon as a child process, and a child process
// started that way is given a pipe for its stderr. Nothing was reading that
// pipe. A pipe nobody reads fills, and the write that fills it does not fail —
// it waits, for as long as the parent runs.
//
// On its own that costs a little lost output. What made it cost everything is
// how logrus is built: Entry.write takes the logger's mutex and holds it
// across the write to Out, and Entry.fireHooks needs the same mutex. So the
// first line that blocked stopped *every* later line in the process, on every
// goroutine, before any of them reached a hook — which is why the log file
// this daemon keeps stopped at the same instant, mid-sentence, and looked like
// a daemon that had simply stopped having anything to say.
//
// The cost lands where it does because of what a sensor loop opens with. The
// first statement inside each one is a log line. Five loops were started, five
// loops reached that line, and none of them reached its sensor. The manager
// records a sensor as running before its loop runs, so the panel on the phone
// and the desktop both said the machine was watching five things. It was
// watching nothing, and it said so to nobody.
//
// The application that opened the pipe now reads it, which is where that bug
// was fixed. This package is the other half: no output this daemon is given
// can stop it watching, whoever starts it and whatever they do with its
// streams. A supervisor that stops reading, a console the user has paused by
// selecting text in it, a log server that stops answering — none of them are
// this program's business, and none of them get to decide whether it watches.
package logsink

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// DefaultDepth is how many lines may wait for the writer before the next one
// is dropped.
//
// Deep enough that an ordinary burst — a start-up, an arm, a sensor failing
// and coming back — is never lost to a writer that is merely slow. Shallow
// enough that a writer which has stopped for good is not holding megabytes of
// text nobody will ever read.
const DefaultDepth = 256

// Writer hands each line to a goroutine of its own and returns immediately.
//
// It satisfies io.Writer, so it goes where logrus expects one, and it never
// returns an error: a logger told that its own output failed can only report
// that through the logger, and this is the thing the logger writes to.
type Writer struct {
	lines   chan []byte
	done    chan struct{}
	stop    sync.Once
	dropped atomic.Int64
}

// New starts a sink that writes to out, holding at most depth lines. A depth
// of zero or less takes [DefaultDepth].
func New(out io.Writer, depth int) *Writer {
	if depth <= 0 {
		depth = DefaultDepth
	}
	w := &Writer{
		lines: make(chan []byte, depth),
		done:  make(chan struct{}),
	}
	go w.run(out)
	return w
}

// Write queues p and returns. It never blocks and never fails.
//
// The bytes are copied because they are not this package's to keep: logrus
// formats into a buffer it reuses, so the slice handed here holds the next
// line by the time the goroutine below gets to it.
func (w *Writer) Write(p []byte) (int, error) {
	line := make([]byte, len(p))
	copy(line, p)

	select {
	case w.lines <- line:
	default:
		// The queue is full, which means the writer is not keeping up or has
		// stopped altogether. Dropping is the price of the guarantee at the
		// top of this file, and it is paid out loud — see run.
		w.dropped.Add(1)
	case <-w.done:
		// Closed. A line that arrives after the end has nowhere to go, and
		// saying so would print a complaint through the logger that is trying
		// to print.
	}
	return len(p), nil
}

// run drains the queue into out, forever, on its own goroutine. This is the
// one place allowed to block: it is what the rest of the program is being kept
// away from.
func (w *Writer) run(out io.Writer) {
	for {
		select {
		case <-w.done:
			return
		case line := <-w.lines:
			// Said before the line it interrupted, and said at all, because a
			// log that quietly leaves things out is worse than one with a gap
			// in it somebody can see. The count is taken and cleared in one
			// step, so lines dropped while this is being written are counted
			// against the next notice rather than lost.
			if n := w.dropped.Swap(0); n > 0 {
				_, _ = fmt.Fprintf(out, "... %d log lines were dropped: the log is not keeping up ...\n", n)
			}
			// The error is dropped deliberately. There is nowhere to report it
			// that does not lead back here.
			_, _ = out.Write(line)
		}
	}
}

// Close stops the goroutine. Safe to call more than once, because shutdown
// paths run twice often enough that it is worth saying so.
//
// Whatever is still queued is left unwritten: this is called when the program
// is ending, and the alternative is a shutdown that waits on the very writer
// this package exists to not wait on.
func (w *Writer) Close() error {
	w.stop.Do(func() { close(w.done) })
	return nil
}
