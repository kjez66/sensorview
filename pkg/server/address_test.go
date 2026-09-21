package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/kjez66/sensorview/pkg/lan"
)

func TestNewDefaultsToLoopback(t *testing.T) {
	// The existing caller renders through headless Chrome on this machine and
	// must keep its loopback-only listener.
	srv := New(t.TempDir())

	if srv.addr != LoopbackAddress {
		t.Errorf("addr = %q, want %q", srv.addr, LoopbackAddress)
	}
}

func TestNewWithAddressBindsThatAddress(t *testing.T) {
	srv := NewWithAddress(t.TempDir(), "127.0.0.1:0")
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	host, _, err := net.SplitHostPort(srv.Address())
	if err != nil {
		t.Fatalf("Address() = %q: %v", srv.Address(), err)
	}
	if host != "127.0.0.1" {
		t.Errorf("bound host = %q, want 127.0.0.1", host)
	}
}

func TestAddressReportsEveryInterfaceBinding(t *testing.T) {
	// A display server for a tablet has to be able to leave loopback.
	srv := NewWithAddress(t.TempDir(), "0.0.0.0:0")
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	// Windows hands back a dual-stack socket, so the wildcard reads as [::]
	// rather than the 0.0.0.0 that was asked for.
	host, _, err := net.SplitHostPort(srv.Address())
	if err != nil {
		t.Fatalf("Address() = %q: %v", srv.Address(), err)
	}
	if host != "0.0.0.0" && host != "::" {
		t.Errorf("bound host = %q, want a wildcard", host)
	}
	if srv.Port() == 0 {
		t.Error("Port() = 0 after Start")
	}
}

func TestAddressEmptyBeforeStart(t *testing.T) {
	if got := NewWithAddress(t.TempDir(), "127.0.0.1:0").Address(); got != "" {
		t.Errorf("Address() = %q before Start, want empty", got)
	}
}

func TestStartRejectsUnusableAddress(t *testing.T) {
	srv := NewWithAddress(t.TempDir(), "256.256.256.256:1")

	if err := srv.Start(); err == nil {
		_ = srv.Stop()
		t.Error("Start succeeded on an unusable address, want an error")
	}
}

func TestServerServesOverTheConfiguredAddress(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>panel</h1>"), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	srv := NewWithAddress(dir, "127.0.0.1:0")
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	response, err := http.Get("http://" + srv.Address() + "/index.html")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", response.StatusCode)
	}
}

func TestServerReachableFromTheNetwork(t *testing.T) {
	// The point of a configurable address: a tablet elsewhere on the network
	// can fetch the theme. Approximated here by using this host's own LAN
	// address rather than loopback.
	interfaces, err := lan.SystemInterfaces()
	if err != nil {
		t.Skipf("no interfaces: %v", err)
	}
	addresses := lan.Addresses(interfaces)
	if len(addresses) == 0 {
		t.Skip("host has no non-loopback IPv4 address")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>panel</h1>"), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	srv := NewWithAddress(dir, "0.0.0.0:0")
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	url := fmt.Sprintf("http://%s:%d/index.html", addresses[0].IP, srv.Port())
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d from %s, want 200", response.StatusCode, url)
	}
	t.Logf("served over %s", url)
}
