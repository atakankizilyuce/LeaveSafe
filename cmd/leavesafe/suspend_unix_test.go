//go:build !windows

package main

import (
	"context"
	"os"
	"strings"
	"syscall"
	"testing"
)

// stubStopProcess stands in for the raise that suspends the process, and hands
// back what it was asked to do.
//
// A test that let the real one run would stop the test binary with nothing left
// in the run to continue it.
func stubStopProcess(t *testing.T) *int {
	t.Helper()

	calls := 0
	real := stopProcess
	stopProcess = func(chan os.Signal) { calls++ }
	t.Cleanup(func() { stopProcess = real })
	return &calls
}

// The point of the whole file: the shell gets its terminal back before the
// process stops, and the dashboard is put back when it resumes. In that order,
// which is what the sequence of escapes shows.
func TestSuspendHandsTheTerminalBackAndTakesItAgain(t *testing.T) {
	var out syncBuffer
	terminalScreen.enter(&out)
	t.Cleanup(terminalScreen.restore)
	stopped := stubStopProcess(t)

	painted := 0
	suspend(make(chan os.Signal, 1), &out, func() { painted++ })

	written := out.String()
	off := strings.Index(written, altScreenOff)
	back := strings.LastIndex(written, altScreenOn)
	switch {
	case off < 0:
		t.Fatalf("the terminal was never handed back: %q", written)
	case back <= off:
		t.Fatalf("the dashboard's screen was not taken again after giving it back: %q", written)
	}
	if !strings.Contains(written[:off], scrollWhole) {
		t.Error("the scrolling region was still pinned when the shell got the terminal back")
	}
	if *stopped != 1 {
		t.Errorf("the process was stopped %d times, want once", *stopped)
	}
	if painted != 1 {
		t.Errorf("the dashboard was redrawn %d times on resume, want once", painted)
	}
	if !terminalScreen.active() {
		t.Error("the program came back without its screen")
	}
}

// A -plain run has a terminal it never took. Coming back from Ctrl+Z it must
// not start drawing on one — there is no dashboard, and switching to the
// alternate screen would hide the shell the user is looking at.
func TestSuspendTakesNothingBackWhenItHeldNothing(t *testing.T) {
	var out syncBuffer
	stopped := stubStopProcess(t)

	painted := 0
	suspend(make(chan os.Signal, 1), &out, func() { painted++ })

	if written := out.String(); written != "" {
		t.Errorf("a run with no dashboard wrote escapes to the terminal: %q", written)
	}
	if painted != 0 {
		t.Errorf("a run with no dashboard was repainted %d times", painted)
	}
	if *stopped != 1 {
		t.Errorf("the process was stopped %d times, want once", *stopped)
	}
}

// The loop keeps answering: Ctrl+Z is not a thing you may only do once.
func TestTheSuspendLoopAnswersEverySignal(t *testing.T) {
	// Each signal is waited for before the next is sent, and the cancellation
	// comes after both. A select with two ready cases picks between them at
	// random, so a loop given everything at once could return on the first one
	// and prove nothing.
	handled := make(chan struct{})
	real := stopProcess
	stopProcess = func(chan os.Signal) { handled <- struct{}{} }
	t.Cleanup(func() { stopProcess = real })

	ch := make(chan os.Signal, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		watchSuspendOn(ctx, ch, &syncBuffer{}, func() {})
	}()

	for range 2 {
		ch <- syscall.SIGTSTP
		<-handled
	}

	cancel()
	<-done
}

// Shutdown has to reach it. A watcher that outlived the program would hold a
// signal handler for a dashboard that no longer exists.
func TestTheSuspendWatcherStopsWithTheProgram(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		watchSuspend(ctx, &syncBuffer{}, func() {})
	}()

	<-done
}
