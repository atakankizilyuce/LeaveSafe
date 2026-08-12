package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

// A window that changed size moves every absolute row the dashboard drew at, so
// it has to be drawn for again. Immediately, where the platform will say so.

func TestAResizeRedrawsTheDashboard(t *testing.T) {
	settleFast(t)
	ch := make(chan os.Signal, 1)
	painted := make(chan struct{}, 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchResizeOn(ctx, ch, func() { painted <- struct{}{} })

	ch <- syscall.Signal(0)

	select {
	case <-painted:
	case <-time.After(2 * time.Second):
		t.Fatal("the window changed size and the dashboard was never redrawn")
	}
}

// Dragging a window edge produces a signal per column crossed. Redrawing on
// each one clears and repaints the screen dozens of times a second, which is
// visible as a flicker for as long as the drag lasts.
func TestADragRedrawsOnceWhenItStops(t *testing.T) {
	settleFast(t)
	ch := make(chan os.Signal, 1)
	painted := make(chan struct{}, 16)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchResizeOn(ctx, ch, func() { painted <- struct{}{} })

	for range 10 {
		ch <- syscall.Signal(0)
	}

	select {
	case <-painted:
	case <-time.After(2 * time.Second):
		t.Fatal("the drag ended and the dashboard was never redrawn")
	}

	// Nothing more should follow it: the ten signals were one gesture.
	select {
	case <-painted:
		t.Error("a single drag redrew the dashboard more than once")
	case <-time.After(4 * resizeSettle):
	}
}

// Shutdown has to reach it. A watcher left running would hold a signal handler
// for a dashboard that no longer exists.
func TestTheResizeWatcherStopsWithTheProgram(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		watchResize(ctx, func() { t.Error("a canceled watcher redrew the dashboard") })
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher outlived the program")
	}
}

// A resize that arrives while an earlier one is still settling restarts the
// wait rather than being lost, and a second gesture after the first has been
// drawn is drawn too.
func TestASecondGestureIsAnsweredAsWell(t *testing.T) {
	settleFast(t)
	ch := make(chan os.Signal, 1)
	painted := make(chan struct{}, 8)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchResizeOn(ctx, ch, func() { painted <- struct{}{} })

	for range 2 {
		ch <- syscall.Signal(0)
		select {
		case <-painted:
		case <-time.After(2 * time.Second):
			t.Fatal("a resize went unanswered")
		}
	}
}

// settleFast shortens the wait for a drag to stop, so the tests above take
// milliseconds rather than a second apiece.
func settleFast(t *testing.T) {
	t.Helper()

	real := resizeSettle
	resizeSettle = 10 * time.Millisecond
	t.Cleanup(func() { resizeSettle = real })
}
