package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leavesafe/leavesafe/internal/auth"
)

// The commands below are the only way a user with no dashboard — a terminal too
// small to draw one, or a session over ssh — can read the address to open, check
// the certificate, or invalidate a key that has been shared too widely. They
// report through the logger in headless mode, which is what these tests read.

const (
	testKey    = "1111-1111-1111-1116"
	testRawKey = "1111111111111116"
)

// dashboardWith builds a headless dashboard holding the addresses, certificate
// and key file that a console command reads.
func dashboardWith(urls []string, certFP, keyPath string) *statusBar {
	return newHeadlessStatusBar(nil, nil, testKey, testRawKey, urls, certFP, keyPath)
}

func TestConsoleUrlsListsEveryAddressItCanOffer(t *testing.T) {
	urls := []string{"http://192.168.1.10:8080", "https://198.51.100.4:9443"}
	sb := dashboardWith(urls, "", "")

	out := consoleOutput(t, "urls\n", consoleDeps{hub: testHub(t), sb: sb})

	for _, u := range urls {
		if !strings.Contains(out, u) {
			t.Errorf("urls did not list %s; output was:\n%s", u, out)
		}
	}
	// Listing them is only half of it: the numbers are what `qr <n>` takes, so
	// the list has to say that is what they are for.
	if !strings.Contains(out, "qr <n>") {
		t.Errorf("urls did not say how to show one of them; output was:\n%s", out)
	}
}

func TestConsoleCertPrintsTheFingerprintToCompare(t *testing.T) {
	const fp = "AA:BB:CC:DD:EE:FF"
	sb := dashboardWith([]string{"https://192.168.1.10:8443"}, fp, "")

	out := consoleOutput(t, "cert\n", consoleDeps{hub: testHub(t), sb: sb})

	if !strings.Contains(out, fp) {
		t.Errorf("cert did not print the fingerprint; output was:\n%s", out)
	}
	// The fingerprint on its own is a number nobody knows what to do with. The
	// phone will warn that the certificate is untrusted, and the point of the
	// command is telling the user that comparing the two is the check.
	if !strings.Contains(out, "self-signed") {
		t.Errorf("cert did not explain what to compare it against; output was:\n%s", out)
	}
}

// Running over plain HTTP is a different situation from having a certificate
// nobody has checked, and saying "no fingerprint" would read as the latter.
func TestConsoleCertOnAPlainHTTPServerSaysThereIsNone(t *testing.T) {
	sb := dashboardWith([]string{"http://192.168.1.10:8080"}, "", "")

	out := consoleOutput(t, "cert\n", consoleDeps{hub: testHub(t), sb: sb})

	if !strings.Contains(out, "No TLS certificate") {
		t.Errorf("cert did not report the absence of a certificate; output was:\n%s", out)
	}
}

// Rotating the key has to reach the file it is stored in. Without that the next
// start would read the key that was just invalidated and come back with it,
// which is the opposite of what the user asked for.
func TestConsoleRotateKeyWritesTheNewKeyToItsFile(t *testing.T) {
	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "pairing.key")
	if err := auth.SaveKeyFile(keyPath, authMgr.RawPairingKey()); err != nil {
		t.Fatalf("seed the key file: %v", err)
	}
	before, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read the seeded key file: %v", err)
	}
	sb := dashboardWith([]string{"http://192.168.1.10:8080"}, "", keyPath)

	out := consoleOutput(t, "rotate-key\n", consoleDeps{
		hub:     testHub(t),
		sb:      sb,
		authMgr: authMgr,
	})

	if !strings.Contains(out, "Pairing key rotated") {
		t.Fatalf("rotate-key did not report a rotation; output was:\n%s", out)
	}
	after, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read the rotated key file: %v", err)
	}
	if string(after) == string(before) {
		t.Error("the stored key still holds the one that was just invalidated")
	}
	// The file is written with a trailing newline, so compare the key itself
	// rather than the bytes around it.
	stored := strings.TrimSpace(string(after))
	if want := strings.ReplaceAll(authMgr.RawPairingKey(), "-", ""); stored != want {
		t.Errorf("the stored key is %q, but the manager now holds %q", stored, want)
	}
	// The dashboard has to show the key the QR code now encodes, or the two
	// disagree on screen.
	if !strings.Contains(out, "All existing sessions invalidated") {
		t.Errorf("rotate-key did not say the old sessions are gone; output was:\n%s", out)
	}
}

// Started from an autostart entry there is no dashboard, so the log is the only
// place the address and the certificate appear at all.
func TestHeadlessStartupLogsEveryAddressAndTheCertificate(t *testing.T) {
	const fp = "AA:BB:CC:DD"
	urls := []string{"http://192.168.1.10:8080", "https://198.51.100.4:9443"}
	keyPath := filepath.Join(t.TempDir(), "pairing.key")
	sb := dashboardWith(urls, fp, keyPath)

	out := captureLog(t, func() { logHeadlessStartup(sb, nil, fp) })

	for _, u := range urls {
		if !strings.Contains(out, "Reachable at "+u) {
			t.Errorf("the startup log did not name %s; output was:\n%s", u, out)
		}
	}
	if !strings.Contains(out, fp) {
		t.Errorf("the startup log did not carry the fingerprint; output was:\n%s", out)
	}
	// The key is deliberately not in the log — logs get pasted into bug
	// reports — so the log says where it lives instead. The path is matched by
	// its file name, because the logger escapes the separators in a Windows
	// path and the full string would never appear verbatim.
	if !strings.Contains(out, "Pairing key is stored in") ||
		!strings.Contains(out, filepath.Base(keyPath)) {
		t.Errorf("the startup log did not say where the key is stored; output was:\n%s", out)
	}
	if strings.Contains(out, testRawKey) || strings.Contains(out, testKey) {
		t.Errorf("the startup log leaked the pairing key; output was:\n%s", out)
	}
}
