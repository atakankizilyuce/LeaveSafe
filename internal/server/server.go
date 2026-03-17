package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/web"
	"nhooyr.io/websocket"
)

// Config holds server configuration.
type Config struct {
	Hub  *ws.Hub
	Port int // 0 means pick a free port automatically
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
	mux.Handle("/", http.FileServer(http.FS(web.StaticFiles())))
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
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.listener = ln
	log.Printf("[INFO] HTTP server bound to port %d", s.port)
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
	urls := make([]string, 0, len(ips))
	for _, ip := range ips {
		urls = append(urls, fmt.Sprintf("http://%s:%d", ip.String(), s.port))
	}
	return urls
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow connections from any origin (same LAN)
	})
	if err != nil {
		log.Printf("[ERROR] WebSocket accept: %v", err)
		return
	}

	s.hub.HandleConnection(r.Context(), conn)
}

// getLocalIPs returns all non-loopback IPv4 addresses.
func getLocalIPs() []net.IP {
	var ips []net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return []net.IP{net.ParseIP("127.0.0.1")}
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			ips = append(ips, ipNet.IP)
		}
	}
	if len(ips) == 0 {
		ips = append(ips, net.ParseIP("127.0.0.1"))
	}
	return ips
}
