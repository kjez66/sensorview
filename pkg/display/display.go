// Package display serves a built theme over HTTP with live sensor data, so a
// browser on another device can act as the panel. Unlike the dev server it
// needs no Node toolchain, and unlike the renderer it needs no USB device.
package display

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/kjez66/sensorview/pkg/lan"
	"github.com/kjez66/sensorview/pkg/sensors"
	"github.com/kjez66/sensorview/pkg/server"
)

const (
	// DefaultAddress serves on loopback only. Reaching the panel from another
	// device is opt-in, because the sensor feed carries no authentication.
	DefaultAddress = "127.0.0.1:19847"

	// DefaultInterval matches the dev server cadence.
	DefaultInterval = time.Second

	readyPollInterval = 5 * time.Millisecond
)

// errNoTheme reports that the theme has not been built.
var errNoTheme = errors.New("theme is not built")

// Options configures a display server.
type Options struct {
	// DistDir is the built theme directory, as produced by theme build.
	DistDir string

	// Address is the host:port to listen on. Defaults to DefaultAddress.
	Address string

	// Interval is how often sensors are collected and broadcast.
	Interval time.Duration

	// SensorOptions are provider options, as passed with --opt.
	SensorOptions map[string]interface{}

	// OnReady, when set, is called once the listener is up and before the first
	// broadcast, so a caller can report the addresses it is reachable at.
	OnReady func(*Server)
}

// URLEntry is one address the panel can be opened at.
type URLEntry struct {
	Interface string
	URL       string
}

// Server serves a built theme with live sensor data.
type Server struct {
	options   Options
	collector *sensors.Collector

	mu   sync.Mutex
	http *server.Server
}

// New validates the options and prepares a server. It does not listen yet.
func New(options Options) (*Server, error) {
	if options.Address == "" {
		options.Address = DefaultAddress
	}
	if options.Interval <= 0 {
		options.Interval = DefaultInterval
	}

	// Checked up front so the error names the fix rather than serving 404s.
	index := filepath.Join(options.DistDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		return nil, fmt.Errorf("%w: no index.html in %s, run theme build first", errNoTheme, options.DistDir)
	}

	return &Server{
		options: options,
		collector: sensors.NewCollector(&sensors.Config{
			Options: options.SensorOptions,
		}),
	}, nil
}

// Run serves until the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	httpServer := server.NewWithAddress(s.options.DistDir, s.options.Address)
	if err := httpServer.Start(); err != nil {
		return fmt.Errorf("listen on %s: %w", s.options.Address, err)
	}

	s.mu.Lock()
	s.http = httpServer
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.http = nil
		s.mu.Unlock()
		_ = httpServer.Stop()
	}()

	if s.options.OnReady != nil {
		s.options.OnReady(s)
	}

	// Prime the collector so the first broadcast carries rates rather than the
	// zeroes a first sample produces.
	s.collector.CollectAll()

	ticker := time.NewTicker(s.options.Interval)
	defer ticker.Stop()

	for {
		// Broadcast failures mean a client went away mid-write, which the read
		// loop cleans up; they are not a reason to stop serving.
		_ = httpServer.BroadcastSensorData(s.collector.CollectAll())

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Address returns the host:port being served, or an empty string before Run.
func (s *Server) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.http == nil {
		return ""
	}
	return s.http.Address()
}

// Port returns the port being served, or zero before Run.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.http == nil {
		return 0
	}
	return s.http.Port()
}

// LocalURL returns the address to open on this machine, or an empty string when
// the listener does not accept loopback connections because it is bound to one
// specific network address. Use URLs in that case.
func (s *Server) LocalURL() string {
	ip, port, ok := listeningIP(s.Address())
	if !ok || !(ip.IsLoopback() || ip.IsUnspecified()) {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d/?ws=%d", port, port)
}

// Exposed reports whether other devices can connect, which means the
// unauthenticated sensor feed is readable from the network.
func (s *Server) Exposed() bool {
	ip, _, ok := listeningIP(s.Address())
	return ok && !ip.IsLoopback()
}

// listeningIP splits a listener address into its IP and port.
func listeningIP(address string) (net.IP, int, bool) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, 0, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port == 0 {
		return nil, 0, false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, 0, false
	}
	return ip, port, true
}

// URLs lists the addresses another device on the network can open. The page and
// its WebSocket share one listener, so each URL points the theme at the port it
// was loaded from. Only addresses the listener actually accepts are listed, so a
// server on loopback reports none.
func (s *Server) URLs() []URLEntry {
	address := s.Address()
	if address == "" {
		return nil
	}

	interfaces, err := lan.SystemInterfaces()
	if err != nil {
		return nil
	}

	return reachableURLs(address, lan.Addresses(interfaces))
}

// boundInterfaceLabel names a listening address that no interface reports, such
// as an IPv6 or link-local one, so it can still be printed.
const boundInterfaceLabel = "listening address"

// reachableURLs narrows the host's LAN addresses to those a listener on address
// accepts connections on: none on loopback, all of them on a wildcard, and only
// the matching one on a specific address.
func reachableURLs(address string, addresses []lan.Address) []URLEntry {
	ip, port, ok := listeningIP(address)
	if !ok || ip.IsLoopback() {
		return nil
	}

	var matched []lan.Address
	if ip.IsUnspecified() {
		matched = addresses
	} else {
		for _, candidate := range addresses {
			if candidate.IP == ip.String() {
				matched = append(matched, candidate)
			}
		}
		if len(matched) == 0 {
			matched = []lan.Address{{Interface: boundInterfaceLabel, IP: ip.String()}}
		}
	}

	entries := make([]URLEntry, 0, len(matched))
	for _, candidate := range matched {
		entries = append(entries, URLEntry{
			Interface: candidate.Interface,
			URL:       lan.URL(candidate, port, port),
		})
	}
	return entries
}

// waitReady blocks until the listener is up, for tests and callers that print
// the address immediately after starting.
func (s *Server) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.Address() != "" {
			return nil
		}
		time.Sleep(readyPollInterval)
	}
	return fmt.Errorf("display server not listening after %s", timeout)
}
