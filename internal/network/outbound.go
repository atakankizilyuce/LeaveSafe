package network

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// PublicOnlyClient returns an HTTP client that refuses to open a connection to
// any address that is not a public unicast IP.
//
// It exists for the outbound requests whose destination the user can point
// somewhere: the two geolocation endpoints. Both are configurable — the Wi-Fi
// one from the phone's own settings screen — and both are fetched by the laptop
// itself. Without this a paired phone could aim geolocate_url at an address on
// the laptop's own network and use the laptop as a blind probe of hosts the
// phone cannot reach directly; https-only, already enforced elsewhere, bounds
// the protocol but not the host.
//
// The check sits in the dialer's Control hook rather than on the URL string,
// and that placement is the point. Control runs once per resolved address,
// immediately before the socket connects, so a hostname that resolves to an
// internal address — the DNS-rebinding shape of this attack — is caught at the
// moment of truth rather than at parse time when the name still looked public.
// A redirect re-dials, so every hop is checked, not just the first.
func PublicOnlyClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: timeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			return guardOutboundAddress(address)
		},
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   timeout,
			ResponseHeaderTimeout: timeout,
			ExpectContinueTimeout: time.Second,
		},
	}
}

// guardOutboundAddress refuses a resolved connect target that is not a public
// unicast address. address is the host:port the dialer is about to connect to,
// with the host already resolved to an IP literal.
func guardOutboundAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Control is handed a resolved literal; anything else is a surprise
		// worth refusing rather than connecting to.
		return fmt.Errorf("refusing to connect to non-address %q", address)
	}
	if !isPublicUnicast(ip) {
		return fmt.Errorf("refusing to connect to non-public address %s", ip)
	}
	return nil
}

// isPublicUnicast reports whether ip is routable on the internet rather than an
// address that only means something on the local machine or network. It is the
// same predicate publicAddr enforces on this machine's own public address,
// pointed the other way — at an outbound destination — and it deliberately
// accepts carrier-grade NAT space for the same reason publicAddr does: refusing
// a syntactically public address is not its job.
func isPublicUnicast(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	return ip.IsGlobalUnicast()
}
