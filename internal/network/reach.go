package network

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/leavesafe/leavesafe/internal/certfp"
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
// This dials the public address and asks what certificate answers. A match is
// proof of two things at once and it is worth being explicit about the second:
// that something completed a TCP connection to that address and port, and that
// the something is this machine rather than the router's own admin page, a
// neighbor on the same public address, or whatever else an ISP has parked
// there. Without the fingerprint, a router that serves its web interface on the
// forwarded port would answer this check and be recorded as success.
//
// A failure proves nothing, and the caller is expected to say so.
func VerifyReachable(addr, wantFP string) error {
	return verifyReachable(addr, wantFP, verifyTimeout)
}

func verifyReachable(addr, wantFP string, timeout time.Duration) error {
	if wantFP == "" {
		return fmt.Errorf("%w: there is no certificate fingerprint to check the answer against", ErrNotVerified)
	}

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", addr, &tls.Config{
		// The certificate is self-signed and always will be, so there is no
		// chain to validate and nothing here to trust on its say-so. It is
		// checked below, by fingerprint, against the one this machine is
		// presenting — which is a stronger statement than a chain would make.
		InsecureSkipVerify: true, //nolint:gosec // G402: checked by fingerprint immediately below
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotVerified, err)
	}
	defer func() { _ = conn.Close() }()

	return whoAnswered(addr, wantFP, conn.ConnectionState().PeerCertificates)
}

// whoAnswered decides whether the certificates presented belong to this
// machine.
//
// Split out because the no-certificate case cannot be produced by a real
// handshake against a real server — a server that presents none does not get
// as far as a connection — and an unreachable branch that dereferences the
// first element of a slice is how a defensive check becomes the crash it was
// written to prevent.
func whoAnswered(addr, wantFP string, certs []*x509.Certificate) error {
	if len(certs) == 0 {
		return fmt.Errorf("%w: %s answered without presenting a certificate", ErrNotVerified, addr)
	}

	if got := certfp.Of(certs[0].Raw); !certfp.Equal(got, wantFP) {
		// Not merely unproven — something else is on that address, and putting
		// a QR code around it would send the pairing key to whatever it is.
		return fmt.Errorf("%s is answering with a different certificate (%s), so it is not this machine",
			addr, got)
	}
	return nil
}
