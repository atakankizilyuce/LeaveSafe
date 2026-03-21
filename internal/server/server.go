package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
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
	Port    int  // 0 means pick a free port automatically
	DevMode bool // serve web assets from filesystem instead of embedded
}

// Server is the HTTP server that serves the web UI and handles WebSocket connections.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	port       int
	hub        *ws.Hub
}

// New creates a new HTTP server.
func New(cfg Config) *Server {
	s := &Server{
		hub:  cfg.Hub,
		port: cfg.Port,
	}

	mux := http.NewServeMux()
	if cfg.DevMode {
		log.Info("Dev mode: serving web assets from filesystem (web/)")
		mux.Handle("/", http.FileServer(http.Dir("web")))
	} else {
		mux.Handle("/", http.FileServer(http.FS(web.StaticFiles())))
	}
	mux.HandleFunc("/ws", s.handleWebSocket)

	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return s
}

// Listen binds to the configured port (or a free port if Port is 0).
// Call this before URLs() or Start().
func (s *Server) Listen() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil && s.port != 0 {
		log.Warnf("Port %d busy, picking a free port", s.port)
		ln, err = net.Listen("tcp", ":0")
	}
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.listener = ln
	log.Infof("HTTP server bound to port %d", s.port)
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

// URLs returns the HTTP URLs clients can connect to.
func (s *Server) URLs() []string {
	ips := getLocalIPs()
	urls := make([]string, 0, len(ips)+1)

	// In container environments, localhost is the primary access point
	// because Docker port mapping forwards host:PORT -> container:PORT.
	if isContainer() {
		urls = append(urls, fmt.Sprintf("http://localhost:%d", s.port))
	}

	for _, ip := range ips {
		urls = append(urls, fmt.Sprintf("http://%s:%d", ip.String(), s.port))
	}
	return urls
}

// Port returns the bound port number.
func (s *Server) Port() int {
	return s.port
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow connections from any origin (same LAN)
	})
	if err != nil {
		log.Errorf("WebSocket accept: %v", err)
		return
	}

	s.hub.HandleConnection(r.Context(), conn)
}

func isContainer() bool {
	return os.Getenv("CONTAINER") == "1"
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
		"docker", "br-", "veth",    // Docker
		"vEthernet",                 // Hyper-V / WSL
		"virbr",                     // libvirt
		"VirtualBox", "vboxnet",     // VirtualBox
		"vmnet",                     // VMware
		"ham",                       // Hamachi
	}
	lower := strings.ToLower(name)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
