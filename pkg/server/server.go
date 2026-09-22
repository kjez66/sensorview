// Package server provides HTTP server for theme rendering with WebSocket sensor data.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// LoopbackAddress binds a random port on the loopback interface, which is all
// the headless renderer on this machine needs.
const LoopbackAddress = "127.0.0.1:0"

// Server serves theme files and streams sensor data via WebSocket.
type Server struct {
	mu       sync.Mutex
	listener net.Listener
	server   *http.Server
	distDir  string
	addr     string

	// hub holds the WebSocket clients of the current listener. Each Start gets a
	// fresh one, because Stop closes it for good.
	hub *Hub

	upgrader websocket.Upgrader
}

// New creates a new theme server on the loopback interface.
func New(distDir string) *Server {
	return NewWithAddress(distDir, LoopbackAddress)
}

// NewWithAddress creates a theme server bound to addr, given as host:port. Use
// an empty or zero port for any free one, and a wildcard host such as
// "0.0.0.0:19847" to reach the panel from another device on the network.
func NewWithAddress(distDir, addr string) *Server {
	return &Server{
		distDir: distDir,
		addr:    addr,
		hub:     NewHub(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for local development
			},
		},
	}
}

// Start starts the server on its configured address.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return fmt.Errorf("server already running")
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = listener

	hub := NewHub()
	s.hub = hub

	mux := http.NewServeMux()

	// Serve static files from dist directory
	fs := http.FileServer(http.Dir(s.distDir))
	mux.Handle("/", fs)

	// WebSocket endpoint for sensor data
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		s.handleWebSocket(w, r, hub)
	})

	s.server = &http.Server{
		Handler: mux,
	}

	go s.server.Serve(s.listener)
	return nil
}

// Port returns the port the server is listening on.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener == nil {
		return 0
	}
	// Comma-ok: a bare assertion would panic if the listener were ever not TCP.
	tcpAddr, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return tcpAddr.Port
}

// Address returns the host:port the server is listening on, or an empty string
// before it starts.
func (s *Server) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// URL returns the base URL of the server.
func (s *Server) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.Port())
}

// Stop stops the server.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close all WebSocket clients
	s.hub.Close()

	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.server.Shutdown(ctx)
		s.server = nil
	}

	if s.listener != nil {
		s.listener.Close()
		s.listener = nil
	}

	return nil
}

// handleWebSocket handles WebSocket connections for sensor data, serving each
// until it disconnects.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request, hub *Hub) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	hub.Serve(conn)
}

// currentHub returns the hub of the running listener.
func (s *Server) currentHub() *Hub {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hub
}

// BroadcastSensorData sends sensor data to all connected WebSocket clients. It
// queues the message and returns without waiting on any client, so one that has
// stopped reading cannot stall the others or the caller.
func (s *Server) BroadcastSensorData(data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	s.currentHub().Broadcast(jsonData)
	return nil
}

// ClientCount returns the number of connected WebSocket clients.
func (s *Server) ClientCount() int {
	return s.currentHub().Len()
}
