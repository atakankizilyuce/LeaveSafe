// Package safe runs the program's background work so that one panicking
// goroutine cannot take the whole process down.
//
// This matters more here than in most programs. LeaveSafe spends its life
// armed and idle: the user has walked away believing the laptop is watched. A
// panic in a sensor poll loop or in the alert dispatcher kills the process, the
// phone's socket drops, and the only signal the user gets is one they are not
// there to see. Recovering, logging and restarting the loop keeps the rest of
// the system watching while the failure is recorded, which is strictly better
// than a silent death.
//
// Nothing here hides bugs: every recovered panic is logged at error level with
// its stack, and reported to the handler installed by SetPanicHandler so it can
// reach the event log and the dashboard.
package safe

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// restartBackoff is how long a supervised loop waits before restarting after a
// panic. It stops a loop that panics immediately on every iteration from
// spinning the CPU, and is short enough that a transient failure costs a
// negligible gap in coverage. It is a var so tests can shorten it.
var restartBackoff = 2 * time.Second

// PanicHandler is notified about every panic this package recovers. name is the
// label passed to Go or Supervise, value is whatever was passed to panic, and
// stack is the stack trace captured at the point of recovery.
type PanicHandler func(name string, value any, stack []byte)

var (
	handlerMu sync.RWMutex
	handler   PanicHandler
)

// SetPanicHandler installs the function notified about recovered panics.
// Passing nil removes the current handler. The handler runs on the panicking
// goroutine, so it should not block; a panic inside the handler itself is
// swallowed rather than allowed to take down the process it exists to protect.
func SetPanicHandler(fn PanicHandler) {
	handlerMu.Lock()
	defer handlerMu.Unlock()
	handler = fn
}

// report logs a recovered panic and forwards it to the installed handler.
func report(name string, value any, stack []byte) {
	log.WithFields(log.Fields{
		"goroutine": name,
		"panic":     value,
	}).Errorf("Recovered from panic in %s — this is a bug, please report it:\n%s", name, stack)

	handlerMu.RLock()
	fn := handler
	handlerMu.RUnlock()
	if fn == nil {
		return
	}
	// The handler exists to surface the failure. If it panics too, there is
	// nothing further to escalate to, and crashing here would defeat the point.
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Panic handler itself panicked: %v", r)
		}
	}()
	fn(name, value, stack)
}

// Recover recovers a panic in the calling function and reports it. Use it as
// the first statement of a deferred call in code that is already running on its
// own goroutine and cannot be restructured to go through Go or Supervise:
//
//	defer safe.Recover("ble-write")
func Recover(name string) {
	if r := recover(); r != nil {
		report(name, r, debug.Stack())
	}
}

// Do runs fn on the current goroutine and reports a panic instead of letting it
// unwind. It returns true if fn completed without panicking. Use it for
// callbacks supplied by another layer, where the caller should survive a bug in
// the callee.
func Do(name string, fn func()) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			report(name, r, debug.Stack())
			ok = false
		}
	}()
	fn()
	return true
}

// Go runs fn on a new goroutine, recovering and reporting any panic. The
// goroutine is not restarted — use it for one-shot work whose failure does not
// leave a gap in monitoring.
func Go(name string, fn func()) {
	go func() {
		defer Recover(name)
		fn()
	}()
}

// Supervise runs fn on a new goroutine and restarts it after a short delay if
// it panics, until ctx is canceled. A normal return from fn ends the
// supervision: the loop is treated as having finished its work rather than
// having failed.
//
// This is what the long-lived loops use — the alert dispatcher, the heartbeat,
// sensor polling. Those must still be running when the user comes back.
func Supervise(ctx context.Context, name string, fn func(context.Context)) {
	SuperviseRetry(ctx, name, func(c context.Context) error {
		fn(c)
		return nil
	})
}

// SuperviseRetry is Supervise for work that can fail without panicking. It
// restarts fn after a short delay when fn panics *or* returns a non-nil error,
// until ctx is canceled. Returning nil ends the supervision.
//
// Panicking is not the only way a loop stops watching, and treating it as the
// only one was a real gap: a sensor whose driver returned an error logged it,
// returned normally, and was retired as though it had finished its work. For a
// program whose whole job is to still be watching when the user comes back,
// "returned an error" and "panicked" deserve the same answer.
func SuperviseRetry(ctx context.Context, name string, fn func(context.Context) error) {
	// Read once, here on the caller's goroutine, rather than on every restart.
	// The supervised goroutine outlives the call, so reading a package variable
	// from inside it would race with a test swapping the value.
	backoff := restartBackoff

	go func() {
		for retryAfter(ctx, name, fn, backoff) {
		}
	}()
}

// retryAfter runs fn once and reports whether it is worth running again, having
// already waited out the backoff if it is.
//
// Three ways to stop: the context ended before the run, fn finished its work
// without failing, or the context ended while it was running or while the
// backoff was being waited out.
func retryAfter(ctx context.Context, name string, fn func(context.Context) error, backoff time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}

	err, panicked := runOnce(ctx, name, fn)
	if !panicked && err == nil {
		return false
	}
	// A cancellation that arrived mid-run is not a failure to retry.
	if ctx.Err() != nil {
		return false
	}

	if panicked {
		log.Warnf("Restarting %s in %v after a panic", name, backoff)
	} else {
		log.Warnf("Restarting %s in %v after an error: %v", name, backoff, err)
	}

	select {
	case <-ctx.Done():
		return false
	case <-time.After(backoff):
		return true
	}
}

// runOnce invokes fn, returning whatever error it produced and whether it
// panicked instead of returning at all.
func runOnce(ctx context.Context, name string, fn func(context.Context) error) (err error, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			report(name, r, debug.Stack())
			panicked = true
		}
	}()
	return fn(ctx), false
}
