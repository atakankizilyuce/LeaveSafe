package network

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/certfp"
)

// listeningServer starts a TLS listener on the loopback with a certificate of
// its own, and returns its address and fingerprint.
//
// A real handshake rather than a fake, because what is being tested is whether
// a connection completes and which certificate answers — and both of those are
// the TLS stack's answers, not this package's.
func listeningServer(t *testing.T) (addr, fingerprint string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate a key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "leavesafe-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create a certificate: %v", err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// The handshake is the whole exchange; nothing here reads or writes.
			go func() {
				defer func() { _ = conn.Close() }()
				if tc, ok := conn.(*tls.Conn); ok {
					_ = tc.Handshake()
				}
			}()
		}
	}()

	return ln.Addr().String(), certfp.Of(der)
}

// The one case that is evidence rather than inference: something completed a
// connection to that address, and the something presented this machine's own
// certificate.
func TestAnAnswerFromOurOwnCertificateIsProof(t *testing.T) {
	addr, fp := listeningServer(t)

	if err := VerifyReachable(addr, fp); err != nil {
		t.Errorf("VerifyReachable = %v, want it to confirm the address", err)
	}
}

// The fingerprint travels without colons in the QR code and with them on the
// dashboard, and both forms have to work here — the check is fed whichever one
// the caller is holding.
func TestTheFingerprintIsAcceptedInEitherForm(t *testing.T) {
	addr, fp := listeningServer(t)

	if err := VerifyReachable(addr, strings.ReplaceAll(fp, ":", "")); err != nil {
		t.Errorf("VerifyReachable with the QR code's form = %v, want it to confirm the address", err)
	}
	if err := VerifyReachable(addr, strings.ToUpper(fp)); err != nil {
		t.Errorf("VerifyReachable uppercased = %v, want it to confirm the address", err)
	}
}

// The reason the certificate is checked at all. A router that serves its own
// admin page on the forwarded port answers this connection perfectly happily,
// and without this it would be recorded as success — after which the dashboard
// would put a QR code around an address belonging to the router, with the
// pairing key in the fragment.
func TestSomethingElseAnsweringIsNotThisMachine(t *testing.T) {
	addr, _ := listeningServer(t)
	const someoneElse = "00:11:22:33:44:55:66:77:88:99:aa:bb:cc:dd:ee:ff"

	err := VerifyReachable(addr, someoneElse)

	if err == nil {
		t.Fatal("a different certificate was accepted as this machine")
	}
	if errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want it reported as the wrong machine rather than as an "+
			"inconclusive check — this one is a finding, not a shrug", err)
	}
}

// Nothing listening is the ordinary failure, and it proves nothing: a router
// that will not let a machine inside reach its own public address produces
// exactly this, and the phone coming in from mobile data never takes that path.
func TestNothingAnsweringProvesNothing(t *testing.T) {
	// Port 1 on the loopback: nothing listens there, and the refusal is
	// immediate rather than a wait for the timeout.
	err := verifyReachable("127.0.0.1:1", "aa:bb", 500*time.Millisecond)

	if err == nil {
		t.Fatal("an address with nothing behind it was reported as reachable")
	}
	if !errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want it to say nothing was confirmed", err)
	}
}

// Asked to check an address against no fingerprint at all, there is nothing to
// compare an answer to — so the honest report is that nothing was established,
// rather than a success on the strength of somebody having answered.
func TestWithoutAFingerprintNothingCanBeProven(t *testing.T) {
	addr, _ := listeningServer(t)

	err := VerifyReachable(addr, "")

	if !errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want it to say nothing was confirmed", err)
	}
}

// A handshake that completes with nothing to identify the far end proves the
// address is answering and nothing about who is answering, so it is reported
// the same way as no answer at all. It cannot be produced against a real
// server, which is exactly why the check is worth having and worth testing:
// the alternative is a branch nobody has ever run reading the first element of
// an empty slice.
func TestAnAnswerWithNoCertificateProvesNothing(t *testing.T) {
	err := whoAnswered("198.51.100.4:9443", "aa:bb", nil)

	if !errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want it to say nothing was confirmed", err)
	}
}
