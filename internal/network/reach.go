package network

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"time"
)

// verifyTimeout is how long the check waits for the round trip out through the
// router and back in. It is short: a connection that has to be waited for is a
// connection that did not happen, and the answer is only ever used to decide
// which address to put a QR code around.
const verifyTimeout = 4 * time.Second

// ErrNotVerified is what an inconclusive check answers.
//
// It is deliberately distinct from "this cannot work". A router that does not
// hairpin — that will not let a machine inside reach its own public address —
// is common and is not broken: the phone coming in from mobile data takes a
// different path and may well arrive. So this says nothing was proven, and the
// caller says so in those words rather than reporting a failure it did not
// observe.
var ErrNotVerified = errors.New("reachability could not be confirmed")

// VerifyReachable answers the question the rest of this package only ever
// guessed at: can a connection from outside this network actually arrive here?
//
// Everything before this is inference. The router accepting a port mapping says
// the router agreed to something, not that a packet completes the journey — the
// host firewall may drop it on the way in, the ISP may block the port, or the
// router may itself be behind another one. The address a lookup service reports
// says where the internet sees this network from, not that anything is listening
// there. Both were being reported as "ACTIVE" with a URL beside them.
//
// This dials the public address and requires that this machine's own
// certificate answer it. A success is proof of two things at once, and the
// second is worth being explicit about: that something completed a TCP
// connection to that address and port, and that the something is this machine
// rather than the router's own admin page, a neighbor on the same public
// address, or whatever else an ISP has parked there. Without that, a router
// serving its web interface on the forwarded port would answer this check and
// be recorded as success.
//
// A failure proves nothing, and the caller is expected to say so.
func VerifyReachable(addr string, cert *x509.Certificate) error {
	return verifyReachable(addr, cert, verifyTimeout)
}

func verifyReachable(addr string, cert *x509.Certificate, timeout time.Duration) error {
	conf, err := pinnedTo(cert)
	if err != nil {
		return err
	}

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", addr, conf)
	if err != nil {
		// Everything arrives here: nothing listening, a firewall swallowing the
		// packet, and something answering that is not this machine. They are
		// not the same finding, but from here they are the same evidence —
		// none — and inventing a distinction would be guessing.
		return fmt.Errorf("%w: %w", ErrNotVerified, err)
	}
	_ = conn.Close()
	return nil
}

// pinnedTo is a TLS configuration that will complete a handshake with one
// certificate in the world and no other.
//
// The certificate is self-signed, so the usual chain to a public authority does
// not exist and there is nothing for the system trust store to say about it.
// What replaces that is stricter rather than looser: this machine's own
// certificate is made the only trusted root, so the ordinary verifier — chain,
// dates, key usage, hostname, and the handshake signature that proves the far
// end holds the private key — has to pass against exactly it.
//
// The name comes out of the certificate rather than from the address dialed,
// and that is the point of doing it this way. The address is a public IP that
// was not known when the certificate was made and is not in its SANs, so asking
// the verifier to match it would fail on every machine. The name is not what
// identifies the far end here; being the one certificate in the pool is.
func pinnedTo(cert *x509.Certificate) (*tls.Config, error) {
	if cert == nil {
		return nil, fmt.Errorf("%w: there is no certificate to check the answer against", ErrNotVerified)
	}

	name := certName(cert)
	if name == "" {
		return nil, fmt.Errorf("%w: the certificate names no host to verify against", ErrNotVerified)
	}

	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{
		RootCAs:    pool,
		ServerName: name,
		MinVersion: tls.VersionTLS12,
	}, nil
}

// certName is a name the certificate carries, for the verifier to match itself
// against.
func certName(cert *x509.Certificate) string {
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0]
	}
	if len(cert.IPAddresses) > 0 {
		return cert.IPAddresses[0].String()
	}
	return ""
}
