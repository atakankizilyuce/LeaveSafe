package main

import (
	"strings"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/paircode"
)

// The dashboard shows two things somebody can carry to a phone: a QR code for
// a camera, and a short code for the application's own field. This is the
// second one — see internal/paircode for what it carries and why.
func TestPairCodeIsBuiltFromTheShownAddress(t *testing.T) {
	sb := dashboardWith([]string{"http://192.168.1.24:9443"}, "")
	sb.qrURLIdx = 0

	code := sb.pairCode()
	if code == "" {
		t.Fatal("the dashboard has an address and a key and produced no code")
	}

	host, port, key, err := paircode.Decode(code)
	if err != nil {
		t.Fatalf("the code the dashboard shows does not decode: %v", err)
	}
	if host != "192.168.1.24" || port != 9443 || key != testRawKey {
		t.Errorf("code carries %s:%d %s, want 192.168.1.24:9443 %s",
			host, port, key, testRawKey)
	}
}

// Two network interfaces mean two addresses and two codes. The one on screen
// has to be the one whose QR is on screen, or somebody reads a code for a
// network they are not on.
func TestPairCodeFollowsTheSelectedAddress(t *testing.T) {
	sb := dashboardWith([]string{
		"http://192.168.1.24:9443",
		"http://10.0.0.7:9443",
	}, "")

	sb.qrURLIdx = 1
	host, _, _, err := paircode.Decode(sb.pairCode())
	if err != nil {
		t.Fatal(err)
	}
	if host != "10.0.0.7" {
		t.Errorf("showing the second address, the code carried %s", host)
	}
}

func TestPairCodeIsAbsentWhenThereIsNothingToBuildItFrom(t *testing.T) {
	// No addresses yet — the listener has not reported. A code built from
	// nothing would be a code that pairs with nothing.
	sb := dashboardWith(nil, "")
	if got := sb.pairCode(); got != "" {
		t.Errorf("with no address the dashboard offered %q", got)
	}
}

func TestPairCodeSkipsAnAddressItCannotEncode(t *testing.T) {
	// A host name or an IPv6 literal. Nothing to show rather than a wrong code.
	sb := dashboardWith([]string{"http://laptop.local:9443"}, "")
	sb.qrURLIdx = 0

	if got := sb.pairCode(); got != "" {
		t.Errorf("an address that cannot be encoded produced %q", got)
	}
}

// refresh takes the mutex and paints inside it, so everything the grid draws
// has to read the fields without taking it again. A sync.Mutex is not
// reentrant: getting this wrong hangs the dashboard on every repaint, which is
// a failure nobody can see and nobody can report.
func TestRepaintingDoesNotDeadlock(t *testing.T) {
	sb := &statusBar{
		out:       &syncBuffer{},
		hub:       testHub(t),
		sensorMgr: monitor.NewManager(),
		key:       testKey,
		rawKey:    testRawKey,
		urls:      []string{"http://192.168.1.24:9443"},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		sb.mu.Lock()
		defer sb.mu.Unlock()
		_ = sb.gridLines()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("drawing the grid under the lock did not finish: it deadlocked")
	}
}

// A headless start has no dashboard, and the log is the only place the user can
// find what they need. The key stays out of it — it has its own owner-only file
// — and so, therefore, does the code, which contains the key.
func TestTheCodeStaysOutOfTheLog(t *testing.T) {
	sb := dashboardWith([]string{"http://192.168.1.24:9443"}, "")
	sb.qrURLIdx = 0
	code := sb.pairCode()
	if code == "" {
		t.Fatal("no code to check for")
	}

	var out strings.Builder
	sb.out = &out
	logHeadlessStartup(sb, "headless")

	if strings.Contains(out.String(), code) {
		t.Error("the pairing code, which carries the key, was written to the log")
	}
}

// The grid is drawn at absolute rows and clipped to the height the layout gave
// it, so its height and its contents have to be worked out from the same
// answer. They were not: the code row was added to the lines without being
// added to the count, and every window drew a status grid with its bottom
// border cut off.
func TestTheGridIsAsTallAsTheLinesItDraws(t *testing.T) {
	for _, tc := range []struct {
		name string
		urls []string
	}{
		{"one address, with a code", []string{"http://192.168.1.24:9443"}},
		{"two addresses, with a code", []string{"http://192.168.1.24:9443", "http://10.0.0.7:9443"}},
		{"a host name, which carries no code", []string{"http://laptop.local:9443"}},
		{"no address at all", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := newHeadlessStatusBar(testHub(t), monitor.NewManager(),
				testKey, testRawKey, tc.urls, "")
			sb.qrURLIdx = 0

			lines := sb.gridLines()
			if got := sb.gridHeight(); got != len(lines) {
				t.Errorf("the layout was told the grid is %d rows and it draws %d: "+
					"the last %d would be clipped", got, len(lines), len(lines)-got)
			}
			if len(lines) > 0 && !strings.HasPrefix(lines[len(lines)-1], "└") {
				t.Errorf("the grid does not end on its bottom border: %q", lines[len(lines)-1])
			}
		})
	}
}
