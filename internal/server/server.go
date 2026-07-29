package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
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
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	return s
}

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
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The default check accepts requests with no Origin header (native
		// clients, tests) and browser requests whose Origin matches the Host —
		// which is always true for the embedded UI, since the page and the
		// socket are served from the same address. What it refuses is exactly
		// the attack: some other website open in a browser on this network (or
		// pointed here by DNS rebinding) quietly opening a socket to this
		// server and spending pairing attempts from the victim's own address.
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

// securityHeaders wraps a handler with response headers that pin down what the
// served pages may do. The UI is self-contained — its own scripts, styles and
// socket, plus the opt-in OpenStreetMap embed — so everything else is refused.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// ws:/wss: are listed alongside 'self' because some browsers do not
		// let 'self' match a scheme change from http(s) to ws(s).
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self' ws: wss:; "+
				"frame-src https://www.openstreetmap.org; "+
				"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
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
