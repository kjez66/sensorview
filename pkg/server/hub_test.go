package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// startHub serves hub on a test listener and returns its WebSocket URL.
func startHub(t *testing.T, hub *Hub) string {
	t.Helper()

	upgrader := websocket.Upgrader{}
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		hub.Serve(conn)
	}))
	t.Cleanup(func() {
		hub.Close()
		listener.Close()
	})
	return "ws" + strings.TrimPrefix(listener.URL, "http")
}

// dialHub connects a client and waits until the hub has registered it.
func dialHub(t *testing.T, hub *Hub, url string) *websocket.Conn {
	t.Helper()

	before := hub.Len()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	waitFor(t, "client registered", func() bool { return hub.Len() > before })
	return conn
}

// waitFor polls until condition holds or fails the test after a few seconds.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A client that never reads, such as a phone that went to sleep with its socket
// open, fills its TCP buffers. Broadcasting must keep returning at once, and
// clients that are reading must keep receiving.
func TestBroadcastIsNotStalledByAClientThatStopsReading(t *testing.T) {
	hub := NewHub()
	url := startHub(t, hub)

	dialHub(t, hub, url) // never reads
	healthy := dialHub(t, hub, url)

	received := make(chan []byte, 64)
	go func() {
		for {
			_, message, err := healthy.ReadMessage()
			if err != nil {
				close(received)
				return
			}
			received <- message
		}
	}()

	// 64 MiB in total, far beyond what the socket buffers of the stalled client
	// absorb, so the old write-under-lock broadcast would block here.
	payload := bytes.Repeat([]byte("x"), 1<<20)
	start := time.Now()
	for i := 0; i < 64; i++ {
		message := append([]byte{byte(i)}, payload...)
		hub.Broadcast(message)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("64 broadcasts took %s with a stalled client, want them to return at once", elapsed)
	}

	// The healthy client may miss messages that were dropped while it lagged,
	// but it must see the last one.
	timeout := time.After(10 * time.Second)
	for {
		select {
		case message, ok := <-received:
			if !ok {
				t.Fatal("healthy client was disconnected")
			}
			if message[0] == 63 {
				return
			}
		case <-timeout:
			t.Fatal("healthy client never received the final broadcast")
		}
	}
}

// A write that cannot complete within the write timeout drops that client.
func TestWriteTimeoutDropsAClientThatStopsReading(t *testing.T) {
	hub := NewHub()
	hub.writeTimeout = 100 * time.Millisecond
	url := startHub(t, hub)

	dialHub(t, hub, url) // never reads

	payload := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < 64 && hub.Len() > 0; i++ {
		hub.Broadcast(payload)
		time.Sleep(20 * time.Millisecond)
	}
	waitFor(t, "stalled client dropped", func() bool { return hub.Len() == 0 })
}

// A peer that vanished without closing the socket never answers pings, so it
// is dropped once the pong wait runs out. A client that is reading answers them
// automatically and stays.
func TestUnansweredPingsDropADeadPeer(t *testing.T) {
	hub := NewHub()
	hub.pingInterval = 20 * time.Millisecond
	hub.pongWait = 150 * time.Millisecond
	url := startHub(t, hub)

	dialHub(t, hub, url) // never reads, so never answers a ping
	alive := dialHub(t, hub, url)
	go func() {
		for {
			if _, _, err := alive.ReadMessage(); err != nil {
				return
			}
		}
	}()

	waitFor(t, "dead peer dropped", func() bool { return hub.Len() == 1 })

	// Several pong waits later the reading client is still connected.
	time.Sleep(4 * hub.pongWait)
	if got := hub.Len(); got != 1 {
		t.Errorf("clients = %d after several pong waits, want the reading client to stay", got)
	}
}

func TestEnqueueDiscardsTheOldestMessageWhenFull(t *testing.T) {
	client := &hubClient{send: make(chan []byte, 2)}

	for _, message := range []string{"a", "b", "c"} {
		client.enqueue([]byte(message))
	}

	var got []string
	for len(client.send) > 0 {
		got = append(got, string(<-client.send))
	}
	if strings.Join(got, "") != "bc" {
		t.Errorf("queued = %v, want [b c]", got)
	}
}

func TestClosedHubRefusesNewClients(t *testing.T) {
	hub := NewHub()
	url := startHub(t, hub)
	conn := dialHub(t, hub, url)

	hub.Close()

	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Error("existing client still readable after Close")
	}

	late, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer late.Close()
	_ = late.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := late.ReadMessage(); err == nil {
		t.Error("client connecting after Close was served")
	}
	if got := hub.Len(); got != 0 {
		t.Errorf("clients = %d after Close, want 0", got)
	}
}

func TestServerCanRestartAfterStop(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	defer s.Stop()

	url := strings.Replace(s.URL(), "http://", "ws://", 1) + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	waitFor(t, "client registered after restart", func() bool { return s.ClientCount() == 1 })

	if err := s.BroadcastSensorData(map[string]int{"n": 1}); err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Errorf("read after restart: %v", err)
	}
}
