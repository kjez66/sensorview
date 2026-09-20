package display

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// themeDist writes a minimal built theme and returns its directory.
func themeDist(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>panel</h1>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	return dir
}

// runServer starts a server and stops it when the test ends.
func runServer(t *testing.T, options Options) *Server {
	t.Helper()

	srv, err := New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Run did not return after the context was cancelled")
		}
	})

	if err := srv.waitReady(5 * time.Second); err != nil {
		t.Fatalf("server never became ready: %v", err)
	}
	return srv
}

func TestNewRejectsMissingDistDir(t *testing.T) {
	_, err := New(Options{DistDir: filepath.Join(t.TempDir(), "absent")})

	if err == nil {
		t.Fatal("New succeeded with no built theme, want an error")
	}
	// The fix is to build the theme, so the message has to say so.
	if !strings.Contains(err.Error(), "build") {
		t.Errorf("error = %q, want it to mention building the theme", err)
	}
}

func TestNewRejectsDistDirWithoutIndex(t *testing.T) {
	_, err := New(Options{DistDir: t.TempDir()})

	if err == nil {
		t.Fatal("New succeeded with no index.html, want an error")
	}
}

func TestNewDefaultsToLoopback(t *testing.T) {
	// Reaching the panel from another device is opt-in, so the default must not
	// put sensor readings on the network on its own.
	srv, err := New(Options{DistDir: themeDist(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !strings.HasPrefix(srv.options.Address, "127.0.0.1:") {
		t.Errorf("default address = %q, want a loopback address", srv.options.Address)
	}
}

func TestNewDefaultsToTheProbedPort(t *testing.T) {
	// The theme SDK probes 19847 first, so serving the page and its WebSocket
	// there means the tablet needs no query parameter.
	srv, err := New(Options{DistDir: themeDist(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !strings.HasSuffix(srv.options.Address, ":19847") {
		t.Errorf("default address = %q, want port 19847", srv.options.Address)
	}
}

func TestNewDefaultsTheInterval(t *testing.T) {
	srv, err := New(Options{DistDir: themeDist(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if srv.options.Interval <= 0 {
		t.Errorf("interval = %v, want a positive default", srv.options.Interval)
	}
}

func TestServesTheBuiltTheme(t *testing.T) {
	srv := runServer(t, Options{DistDir: themeDist(t), Address: "127.0.0.1:0"})

	response, err := http.Get("http://" + srv.Address() + "/index.html")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
}

func TestBroadcastsSensorDataOnTheSamePort(t *testing.T) {
	srv := runServer(t, Options{
		DistDir:  themeDist(t),
		Address:  "127.0.0.1:0",
		Interval: 50 * time.Millisecond,
	})

	// The page and the WebSocket share one listener, which is what lets the
	// tablet connect without a ?ws= parameter on the default port.
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Address()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		t.Fatalf("payload %q is not JSON: %v", payload, err)
	}
	if len(data) == 0 {
		t.Error("broadcast carried no sensors")
	}
	if _, ok := data["cpu"]; !ok {
		t.Errorf("broadcast has no cpu reading: %v", data)
	}
}

func TestRunStopsWhenTheContextIsCancelled(t *testing.T) {
	srv, err := New(Options{DistDir: themeDist(t), Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	if err := srv.waitReady(5 * time.Second); err != nil {
		t.Fatalf("server never became ready: %v", err)
	}
	address := srv.Address()
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Run = %v, want nil or context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	// The listener must actually be released, or a restart fails.
	if _, err := http.Get("http://" + address + "/index.html"); err == nil {
		t.Error("server still answering after Run returned")
	}
}

func TestRunRejectsAnAddressInUse(t *testing.T) {
	first := runServer(t, Options{DistDir: themeDist(t), Address: "127.0.0.1:0"})

	second, err := New(Options{DistDir: themeDist(t), Address: first.Address()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := second.Run(context.Background()); err == nil {
		t.Error("Run succeeded on an address already in use, want an error")
	}
}

func TestURLsNameEachReachableAddress(t *testing.T) {
	srv := runServer(t, Options{DistDir: themeDist(t), Address: "0.0.0.0:0"})

	urls := srv.URLs()
	if len(urls) == 0 {
		t.Skip("host has no non-loopback IPv4 address")
	}

	port := srv.Port()
	for _, entry := range urls {
		if !strings.Contains(entry.URL, ":"+itoa(port)+"/") {
			t.Errorf("url %q does not carry the listening port %d", entry.URL, port)
		}
		// Page and socket share the port, so the query parameter must match it.
		if !strings.HasSuffix(entry.URL, "?ws="+itoa(port)) {
			t.Errorf("url %q does not point the theme at the same port", entry.URL)
		}
		if entry.Interface == "" {
			t.Errorf("url %q has no interface name", entry.URL)
		}
	}
}

func TestURLsEmptyBeforeRun(t *testing.T) {
	srv, err := New(Options{DistDir: themeDist(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if urls := srv.URLs(); len(urls) != 0 {
		t.Errorf("URLs = %v before Run, want none", urls)
	}
}

// itoa keeps the URL assertions readable.
func itoa(value int) string {
	return strconv.Itoa(value)
}

func TestOnReadyFiresWithAListeningServer(t *testing.T) {
	ready := make(chan *Server, 1)
	runServer(t, Options{
		DistDir: themeDist(t),
		Address: "127.0.0.1:0",
		OnReady: func(s *Server) { ready <- s },
	})

	select {
	case srv := <-ready:
		// The callback exists so a caller can print the address, so the
		// listener has to be up by the time it runs.
		if srv.Address() == "" {
			t.Error("OnReady ran before the server was listening")
		}
		if srv.Port() == 0 {
			t.Error("OnReady ran with no port assigned")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnReady never ran")
	}
}

func TestOnReadyIsOptional(t *testing.T) {
	// Nil callback must not panic.
	srv := runServer(t, Options{DistDir: themeDist(t), Address: "127.0.0.1:0"})

	if srv.Address() == "" {
		t.Error("server not listening")
	}
}
