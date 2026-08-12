package main

import (
	"context"
	"os"
	"time"
)

// resizeSettle is how long the window has to stop changing before the dashboard
// is drawn again.
//
// Dragging a window edge produces a signal per column, and repainting on each
// one means clearing and redrawing the screen dozens of times a second — which
// is both wasted work and visible as a flicker while the drag is still going
// on. Waiting for the drag to stop costs a fifth of a second in exchange.
var resizeSettle = 200 * time.Millisecond

// watchResize redraws the dashboard when the terminal window changes size.
//
// The layout is worked out from the size of the window, so a window that
// changes size leaves every absolute row the dashboard drew at pointing
// somewhere else: the log's scrolling region ends up over the status grid, and
// the next repaint draws the grid a second time lower down.
//
// The five-second repaint would catch this on its own, because every repaint
// checks the layout still holds before painting a piece of it. This is what
// makes the answer immediate rather than eventual, and it is only half the
// story: Windows has no signal for it and is left with the five seconds.
func watchResize(ctx context.Context, repaint func()) {
	ch := make(chan os.Signal, 1)
	notifyResize(ch)
	defer stopResizeNotices(ch)

	watchResizeOn(ctx, ch, repaint)
}

// watchResizeOn is the loop, over a channel somebody else registered, so that a
// test can deliver a resize without a window to resize.
func watchResizeOn(ctx context.Context, ch chan os.Signal, repaint func()) {
	// nil until a resize arrives, and nil again once one has been drawn for. A
	// receive from a nil channel blocks forever, which is exactly the wanted
	// behavior between gestures: no timer to stop, and nothing left in one to
	// drain.
	var settled <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			// Each signal pushes the repaint further out, so a drag redraws
			// once when it ends rather than once per column crossed. The
			// previous wait is dropped rather than canceled: nothing is
			// listening to it any more, so its firing goes nowhere.
			settled = time.After(resizeSettle)
		case <-settled:
			settled = nil
			repaint()
		}
	}
}
