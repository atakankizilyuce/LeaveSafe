package main

import (
	"strings"
	"sync"
	"testing"
)

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

	const rounds = 200
	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		for range rounds {
			sb.setURLs([]string{"http://192.168.1.10:8080", "https://198.51.100.4:9443"})
		}
	}()
	go func() {
		defer wg.Done()
		for range rounds {
			sb.setKey("2222-2222-2222-2224")
		}
	}()
	go func() {
		defer wg.Done()
		for range rounds {
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
	}()
	go func() {
		defer wg.Done()
		for range rounds {
			sb.setCertFP("CC:DD")
			if fp := sb.certFingerprint(); fp != "AA:BB" && fp != "CC:DD" {
				t.Errorf("read a torn fingerprint: %q", fp)
				return
			}
		}
	}()

	wg.Wait()
}
