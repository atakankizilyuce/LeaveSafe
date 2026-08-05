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

// Four goroutines reach into the dashboard: the console as the user types, the
// five-second repaint, the reachability probe that replaces the URL list a
// minute after startup, and the panic handler. The console's own commands used
// to touch the fields directly — `rotate-key` wrote the key, `urls` and `cert`
// read the address list and the fingerprint — while the others were writing them
// under the mutex.
//
// Under -race this fails outright on the old code. Without it, the assertion
// still holds: a reader has to see a whole key and a whole address, never one
// spliced from either side of a change.
func TestDashboardFieldsSurviveConcurrentReadersAndWriters(t *testing.T) {
	sb := newHeadlessStatusBar(nil, nil, "1111-1111-1111-1116", "1111111111111116",
		[]string{"http://192.168.1.10:8080"}, "AA:BB", "")

	hammers := []func(){
		func() { rewriteURLs(sb) },
		func() { rewriteKey(sb) },
		func() { readURLsWhileTheyChange(t, sb) },
		func() { rewriteAndReadCertFP(t, sb) },
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

// rewriteURLs stands in for the reachability probe, which replaces the whole
// address list at once a minute after startup.
func rewriteURLs(sb *statusBar) {
	for range dashboardRaceRounds {
		sb.setURLs([]string{"http://192.168.1.10:8080", "https://198.51.100.4:9443"})
	}
}

// rewriteKey stands in for the console's `rotate-key` command.
func rewriteKey(sb *statusBar) {
	for range dashboardRaceRounds {
		sb.setKey("2222-2222-2222-2224")
	}
}

// readURLsWhileTheyChange stands in for the repaint and the `urls` command. It
// asserts the two values it reads agree with each other: an index that outruns
// the list it was chosen from means the pair was read across a write.
func readURLsWhileTheyChange(t *testing.T, sb *statusBar) {
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

// rewriteAndReadCertFP stands in for the `cert` command racing the panic
// handler. Either fingerprint is correct; anything else was read mid-write.
func rewriteAndReadCertFP(t *testing.T, sb *statusBar) {
	for range dashboardRaceRounds {
		sb.setCertFP("CC:DD")
		if fp := sb.certFingerprint(); fp != "AA:BB" && fp != "CC:DD" {
			t.Errorf("read a torn fingerprint: %q", fp)
			return
		}
	}
}
