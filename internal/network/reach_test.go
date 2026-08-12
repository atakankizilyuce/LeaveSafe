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
	"testing"
	"time"
)

// listeningServer starts a TLS listener on the loopback with a certificate of
// its own, and returns its address and fingerprint.
//
// A real handshake rather than a fake, because what is being tested is whether
// a connection completes and which certificate answers — and both of those are
// the TLS stack's answers, not this package's.
func listeningServer(t *testing.T) (addr string, cert *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate a key: %v", err)
	}
	// Shaped like the one internal/server issues, down to IsCA being left
	// false: that is what makes "pin it as its own root" a claim worth testing
	// rather than an assumption.
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "leavesafe-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:              []string{"localhost"},
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

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse the certificate: %v", err)
	}
	return ln.Addr().String(), leaf
}

// The one case that is evidence rather than inference: something completed a
// connection to that address, and the something is this machine.
func TestAnAnswerFromOurOwnCertificateIsProof(t *testing.T) {
	addr, cert := listeningServer(t)

	if err := VerifyReachable(addr, cert); err != nil {
		t.Errorf("VerifyReachable = %v, want it to confirm the address", err)
	}
}

// The claim the whole design rests on, and the one that is not obvious: the
// certificate is self-signed and is not a CA, so pinning it as its own root is
// something Go has to actually accept. If a release ever stopped accepting it,
// every reachability check would quietly report "unproven" and no address would
// ever be promoted again — a failure that looks exactly like a network problem.
func TestOurOwnCertificateIsAcceptedAsItsOwnRoot(t *testing.T) {
	_, cert := listeningServer(t)
	if cert.IsCA {
		t.Fatal("the premise of this test is wrong: the certificate is a CA")
	}

	conf, err := pinnedTo(cert)
	if err != nil {
		t.Fatalf("pinnedTo = %v", err)
	}

	if _, err := cert.Verify(x509.VerifyOptions{Roots: conf.RootCAs, DNSName: conf.ServerName}); err != nil {
		t.Errorf("the certificate does not verify against itself: %v", err)
	}
}

// The name is taken from the certificate rather than from the address dialed,
// because the address is a public IP that was not known when the certificate
// was made and is not in its SANs. Verifying against it would fail everywhere.
func TestTheNameCheckedIsOneTheCertificateCarries(t *testing.T) {
	_, cert := listeningServer(t)

	conf, err := pinnedTo(cert)
	if err != nil {
		t.Fatalf("pinnedTo = %v", err)
	}

	if conf.ServerName != "localhost" {
		t.Errorf("ServerName = %q, want a name the certificate carries", conf.ServerName)
	}
	if conf.InsecureSkipVerify {
		t.Error("verification was switched off, which is the thing pinning exists to avoid")
	}
}

// A certificate with no name at all cannot be verified against anything, so the
// honest answer is that nothing was established — not a connection accepted on
// the strength of somebody having answered.
func TestACertificateThatNamesNoHostCannotBeChecked(t *testing.T) {
	if _, err := pinnedTo(&x509.Certificate{}); !errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want it to say nothing was confirmed", err)
	}
	if _, err := pinnedTo(nil); !errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want it to say nothing was confirmed", err)
	}
}

// A certificate with only an IP address still names something to check against.
func TestAnIPAddressIsANameToo(t *testing.T) {
	conf, err := pinnedTo(&x509.Certificate{IPAddresses: []net.IP{net.IPv4(198, 51, 100, 4)}})
	if err != nil {
		t.Fatalf("pinnedTo = %v", err)
	}
	if conf.ServerName != "198.51.100.4" {
		t.Errorf("ServerName = %q, want the address the certificate carries", conf.ServerName)
	}
}

// The reason the certificate is pinned at all. A router that serves its own
// admin page on the forwarded port answers the connection perfectly happily,
// and without this it would be recorded as success — after which the dashboard
// would put a QR code around an address belonging to the router, with the
// pairing key in the fragment.
func TestSomethingElseAnsweringIsNotThisMachine(t *testing.T) {
	addr, _ := listeningServer(t)
	_, someoneElse := listeningServer(t)

	err := VerifyReachable(addr, someoneElse)

	if err == nil {
		t.Fatal("a different certificate was accepted as this machine")
	}
	if !errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want it to say nothing was confirmed", err)
	}
}

// Nothing listening is the ordinary failure, and it proves nothing: a router
// that will not let a machine inside reach its own public address produces
// exactly this, and the phone coming in from mobile data never takes that path.
func TestNothingAnsweringProvesNothing(t *testing.T) {
	_, cert := listeningServer(t)

	// Port 1 on the loopback: nothing listens there, and the refusal is
	// immediate rather than a wait for the timeout.
	err := verifyReachable("127.0.0.1:1", cert, 500*time.Millisecond)

	if err == nil {
		t.Fatal("an address with nothing behind it was reported as reachable")
	}
	if !errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want it to say nothing was confirmed", err)
	}
}

// A check that cannot be set up at all is reported before anything is dialed,
// because there would be nothing to compare the answer to.
func TestWithNothingToPinToNothingIsDialed(t *testing.T) {
	err := verifyReachable("127.0.0.1:1", &x509.Certificate{}, time.Millisecond)

	if !errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want it to say nothing was confirmed", err)
	}
}
