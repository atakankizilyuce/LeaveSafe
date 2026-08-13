package main

import (
	"strings"
	"sync"
	"testing"
)

// dashboardRaceRounds is how many times each goroutine hammers the dashboard:
// high enough that an unsynchronised field is caught mid-write, low enough that
// the test stays well under a second.
const dashboardRaceRounds = 200

// Three goroutines reach into the dashboard: the console as the user types, the
// five-second repaint, and the panic handler. The console's own commands used to
// touch the fields directly — `rotate-key` wrote the key, `urls` read the
// address list and the selection beside it — while the others were reading them
// under the mutex.
//
// Under -race this fails outright on the old code. Without it, the assertion
// still holds: a reader has to see a whole key and a whole address, never one
// spliced from either side of a change.
func TestDashboardFieldsSurviveConcurrentReadersAndWriters(t *testing.T) {
	urls := []string{"http://192.168.1.10:8080", "http://198.51.100.4:8080"}
	sb := newHeadlessStatusBar(nil, nil, "1111-1111-1111-1116", "1111111111111116", urls, "")
	sb.qrCodes = renderQRCodes(urls, "1111111111111116")

	hammers := []func(){
		func() { rewriteKey(sb) },
		func() { moveTheSelection(sb) },
		func() { readURLsWhileTheSelectionMoves(t, sb) },
	}

	var wg sync.WaitGroup
	for _, hammer := range hammers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hammer()
		}()
	}
	wg.Wait()
}

// rewriteKey stands in for the console's `rotate-key` command.
func rewriteKey(sb *statusBar) {
	for range dashboardRaceRounds {
		sb.setKey("2222-2222-2222-2224")
	}
}

// moveTheSelection stands in for the console's `qr <n>` command, which is the
// one thing that still writes to the dashboard while it is being read.
func moveTheSelection(sb *statusBar) {
	for i := range dashboardRaceRounds {
		sb.showQR(i%2 + 1)
	}
}

// readURLsWhileTheSelectionMoves stands in for the repaint and the `urls`
// command. It asserts the two values it reads agree with each other: an index
// that outruns the list it was chosen from means the pair was read across a
// write.
func readURLsWhileTheSelectionMoves(t *testing.T, sb *statusBar) {
	for range dashboardRaceRounds {
		urls, selected := sb.urlListWithSelection()
		for _, u := range urls {
			if !strings.HasPrefix(u, "http") {
				t.Errorf("read a torn address: %q", u)
				return
			}
		}
		if selected >= len(urls) {
			t.Errorf("the selected index %d is past the %d addresses on offer", selected, len(urls))
			return
		}
	}
}
