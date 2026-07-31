package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/web"
	"nhooyr.io/websocket"
)

// Config holds server configuration.
type Config struct {
	Hub     *ws.Hub
	Port    int              // 0 means pick a free port automatically
	DevMode bool             // serve web assets from filesystem instead of embedded
	TLSCert *tls.Certificate // non-nil enables HTTPS/WSS
	CertFP  string           // SHA-256 fingerprint of the TLS certificate
}

// Server is the HTTP server that serves the web UI and handles WebSocket connections.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	port       int
	hub        *ws.Hub
	tlsCert    *tls.Certificate
	certFP     string
	devMode    bool
}

// New creates a new HTTP server.
func New(cfg Config) *Server {
	s := &Server{
		hub:     cfg.Hub,
		port:    cfg.Port,
		tlsCert: cfg.TLSCert,
		certFP:  cfg.CertFP,
		devMode: cfg.DevMode,
	}

	mux := http.NewServeMux()
	if cfg.DevMode {
		// Serves whatever `npm run build` last produced, so a rebuild shows up
		// without restarting the binary. For hot reload instead, run
		// `npm run dev` in web/ — it proxies /ws back to this server.
		log.Info("Dev mode: serving web assets from web/dist")
		mux.Handle("/", http.FileServer(http.Dir("web/dist")))
	} else {
		mux.Handle("/", http.FileServer(http.FS(web.StaticFiles())))
	}
	mux.HandleFunc("/ws", s.handleWebSocket)

	s.httpServer = &http.Server{
		Handler:           requireAddressHost(securityHeaders(mux, cfg.TLSCert != nil), cfg.DevMode),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		// Without this a keep-alive connection is held for as long as the peer
		// cares to hold it. With remote access on, the peer is the internet.
		IdleTimeout:    idleTimeout,
		MaxHeaderBytes: maxHeaderBytes,
	}

	return s
}

// How long the server will wait on a client, and how much header it will read.
//
// These bound the ordinary HTTP path — fetching the page and its assets — and
// they are safe to apply globally even though a paired phone holds its socket
// open for hours: net/http clears the connection's deadlines when a handler
// hijacks it, which is what the WebSocket upgrade does. That is load-bearing
// rather than incidental, so TestWebSocketOutlivesTheServerWriteTimeout holds
// it in place.
//
// The WebSocket is not left unbounded by that. The hub gives an unpaired peer a
// deadline of its own, caps how many of them there may be at once, and limits
// the size of anything any of them sends.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 90 * time.Second
	maxHeaderBytes    = 1 << 16 // 64 KiB
)

// Listen binds to the configured port (or a free port if Port is 0).
// Call this before URLs() or Start().
func (s *Server) Listen() error {
	// #nosec G102 -- binding to all interfaces is the point: the phone connects
	// to this machine over the LAN (or via the UPnP mapping) to reach the alarm.
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil && s.port != 0 {
		log.Warnf("Port %d busy, picking a free port", s.port)
		ln, err = net.Listen("tcp", ":0") // #nosec G102 -- see above
	}
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return fmt.Errorf("listen: unexpected address type %T", ln.Addr())
	}
	s.port = addr.Port

	if s.tlsCert != nil {
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{*s.tlsCert},
			MinVersion:   tls.VersionTLS12,
		}
		ln = tls.NewListener(ln, tlsCfg)
		log.Infof("HTTPS server bound to port %d (TLS enabled)", s.port)
	} else {
		log.Infof("HTTP server bound to port %d", s.port)
	}

	s.listener = ln
	return nil
}

// Start begins serving HTTP connections on the listener opened by Listen.
func (s *Server) Start() error {
	return s.httpServer.Serve(s.listener)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// URLs returns the HTTP(S) URLs clients can connect to.
func (s *Server) URLs() []string {
	scheme := "http"
	if s.tlsCert != nil {
		scheme = "https"
	}

	ips := getLocalIPs()
	urls := make([]string, 0, len(ips))

	for _, ip := range ips {
		urls = append(urls, fmt.Sprintf("%s://%s:%d", scheme, ip.String(), s.port))
	}
	return urls
}

// IsTLS returns whether the server is using TLS.
func (s *Server) IsTLS() bool {
	return s.tlsCert != nil
}

// CertFingerprint returns the SHA-256 fingerprint of the TLS certificate.
func (s *Server) CertFingerprint() string {
	return s.certFP
}

// Port returns the bound port number.
func (s *Server) Port() int {
	return s.port
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// The server's read and write timeouts are set for fetching a page, and a
	// paired phone holds this socket open for hours. Accept hijacks the
	// connection without touching the deadlines already on it, so they have to
	// come off here — before the handover, while there is still a
	// ResponseWriter to ask. Left on, every phone would be disconnected thirty
	// seconds after it paired.
	//
	// The socket does not become unbounded by this: the hub gives an unpaired
	// peer a deadline of its own, caps how many of them there may be, and
	// limits the size of anything they send.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The default check accepts requests with no Origin header (native
		// clients, tests) and browser requests whose Origin matches the Host —
		// which is always true for the embedded UI, since the page and the
		// socket are served from the same address. What it refuses is some
		// other website, open in a browser on this network, quietly opening a
		// socket to this server and spending pairing attempts from the victim's
		// own address.
		//
		// It does NOT refuse DNS rebinding, and reading it as though it did was
		// a mistake worth naming: in a rebinding attack the browser sends the
		// attacker's own domain as both Origin and Host, so the two match and
		// the check passes. requireAddressHost is what closes that, by refusing
		// any Host that is not an address literal.
		//
		// Dev mode is the one legitimate cross-origin case: Vite serves the UI
		// on its own port and proxies /ws here with the browser's Origin intact.
		InsecureSkipVerify: s.devMode,
	})
	if err != nil {
		log.Errorf("WebSocket accept: %v", err)
		return
	}

	s.hub.HandleConnection(r.Context(), conn, r.RemoteAddr)
}

// requireAddressHost refuses any request whose Host header is a DNS name.
//
// This is the DNS rebinding defense, and nothing else in the stack provides it.
// The Origin check on the WebSocket compares Origin against Host, and in a
// rebinding attack both are the attacker's own domain — evil.example, whose
// record has been re-pointed at this machine's LAN address — so they match and
// the socket opens. From there a page the user never trusted is talking to the
// alarm: reading the greeting, spending pairing attempts, and holding the
// unpaired-socket slots the owner's phone needs.
//
// The refusal is exact rather than a heuristic because every address LeaveSafe
// ever hands out is an address literal. The QR code, `urls`, and the certificate
// SANs are all built from net.Interfaces or from the discovered public IP —
// there is no code path that produces a hostname. So a Host that does not parse
// as an IP was not produced by this program, and a browser only sends one when
// something else chose the name.
//
// Dev mode is exempt: Vite serves the UI from localhost on its own port and
// proxies here, and localhost is allowed anyway, but the exemption keeps a
// developer's tunnel or container alias from being confusing to debug.
func requireAddressHost(next http.Handler, devMode bool) http.Handler {
	var mu sync.Mutex
	var lastWarned time.Time

	// The refusal is worth saying out loud — someone who typed a hostname needs
	// to know why nothing loads — but not once per request. Whoever is being
	// refused controls how often they ask, and the application log is
	// size-rotated, so a line per refusal is a way to push the rest of the log
	// out. Once a minute keeps the explanation and drops the flood.
	shouldWarn := func(now time.Time) bool {
		mu.Lock()
		defer mu.Unlock()
		if now.Sub(lastWarned) < hostWarnInterval {
			return false
		}
		lastWarned = now
		return true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !devMode && !isAddressHost(r.Host) {
			if shouldWarn(time.Now()) {
				log.Warnf("Refusing requests for host %q (most recently from %s): LeaveSafe is only "+
					"reachable by IP address, and a hostname here is how a DNS rebinding attack arrives",
					r.Host, r.RemoteAddr)
			}
			// 421 is the status for "this server is not authoritative for the
			// name you asked for", which is precisely the objection.
			http.Error(w, "LeaveSafe is only reachable by IP address", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostWarnInterval is how often a refused hostname is worth a log line.
const hostWarnInterval = time.Minute

// isAddressHost reports whether host is an IP literal or localhost, with or
// without a port.
func isAddressHost(host string) bool {
	if host == "" {
		// HTTP/1.1 requires a Host and Go rejects a request without one before
		// it reaches here. A non-browser client on HTTP/1.0 has no Origin to
		// abuse and no DNS name to rebind, so there is nothing to refuse.
		return true
	}
	h := host
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		h = hostOnly
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	return net.ParseIP(h) != nil
}

// hstsMaxAge is how long a browser should refuse to talk to this origin over
// plain HTTP. Six months, with no preload directive: preloading is for public
// domains, and this is a laptop's address on somebody's LAN.
const hstsMaxAge = 15552000 // 180 days in seconds

// securityHeaders wraps a handler with response headers that pin down what the
// served pages may do. The UI is self-contained — its own scripts, styles and
// socket, plus the opt-in OpenStreetMap embed — so everything else is refused.
//
// tls reports whether the server is actually serving HTTPS. HSTS is only sent
// in that case: sent over plain HTTP it is ignored by browsers per the spec,
// and on the LAN-only path there is no HTTPS listener for a browser to be
// upgraded to, so pinning the origin to HTTPS would lock the user out of their
// own alarm until the pin expired.
func securityHeaders(next http.Handler, tls bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		if tls {
			h.Set("Strict-Transport-Security", fmt.Sprintf("max-age=%d", hstsMaxAge))
		}
		// The page needs none of these. Denying them means a bug in the UI, or
		// anything that ever manages to inject into it, cannot quietly reach
		// the camera, microphone or position of the phone holding it.
		h.Set("Permissions-Policy",
			"accelerometer=(), ambient-light-sensor=(), autoplay=(self), camera=(), "+
				"display-capture=(), encrypted-media=(), fullscreen=(self), geolocation=(self), "+
				"gyroscope=(), magnetometer=(), microphone=(), midi=(), payment=(), "+
				"picture-in-picture=(), publickey-credentials-get=(), screen-wake-lock=(self), "+
				"usb=(), xr-spatial-tracking=()")
		// The phone's own position is the location anchor, so geolocation stays
		// allowed for this origin — it is the one capability the UI genuinely
		// asks for, and only when the user turns the feature on.

		// The socket origin is named explicitly rather than with a bare `ws:`.
		// Some browsers will not let 'self' match a scheme change from http(s)
		// to ws(s), which is why the scheme has to be spelled out at all — but
		// `ws: wss:` spells out every host on the internet along with it, and
		// that is the one channel by which script on this page could ship the
		// pairing key somewhere. Naming this host closes it while leaving the
		// UI, which only ever dials its own origin, working exactly as before.
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self' "+socketOrigins(r, tls)+"; "+
				"frame-src https://www.openstreetmap.org; "+
				"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// socketOrigins returns the WebSocket origins the page is allowed to dial: its
// own host, and nothing else.
//
// The Host header is the address the browser actually asked for, which is the
// address its socket has to match — this machine answers on several (every
// local interface, plus the public one when remote access is on) and a
// certificate or a config file would only name one of them. A request with no
// Host is not one a browser makes; it gets the schemes alone, which is the old
// behavior and no worse than it was.
func socketOrigins(r *http.Request, tls bool) string {
	if r.Host == "" {
		return "ws: wss:"
	}
	if tls {
		return "wss://" + r.Host
	}
	// Plain HTTP still allows wss:// to the same host so that turning on remote
	// access does not require a stale page to be reloaded before it reconnects.
	return "ws://" + r.Host + " wss://" + r.Host
}

// getLocalIPs returns non-loopback IPv4 addresses, skipping virtual
// interfaces commonly created by Docker, WSL, and similar tools.
func getLocalIPs() []net.IP {
	var ips []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return []net.IP{net.ParseIP("127.0.0.1")}
	}
	for _, iface := range ifaces {
		// Skip down, loopback, and virtual/container interfaces.
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
				ips = append(ips, ipNet.IP)
			}
		}
	}
	if len(ips) == 0 {
		ips = append(ips, net.ParseIP("127.0.0.1"))
	}
	return ips
}

// isVirtualInterface returns true for interfaces created by Docker,
// WSL, Hyper-V, VirtualBox, and similar virtualization tools.
func isVirtualInterface(name string) bool {
	prefixes := []string{
		"docker", "br-", "veth", // Docker
		"vEthernet",             // Hyper-V / WSL
		"virbr",                 // libvirt
		"VirtualBox", "vboxnet", // VirtualBox
		"vmnet", // VMware
		"ham",   // Hamachi
	}
	lower := strings.ToLower(name)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
