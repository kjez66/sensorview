package server

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Defaults for Hub. A panel is usually a phone on Wi-Fi, which can sleep or walk
// out of range without closing its socket, so every connection is written with
// a deadline and probed with pings rather than trusted to report its own death.
const (
	// DefaultWriteTimeout bounds one write to one client.
	DefaultWriteTimeout = 10 * time.Second

	// DefaultPingInterval is how often an idle connection is probed. It must be
	// shorter than DefaultPongWait.
	DefaultPingInterval = 25 * time.Second

	// DefaultPongWait is how long a client may stay silent, pongs included,
	// before it is dropped.
	DefaultPongWait = 60 * time.Second

	// DefaultSendQueue is how many messages may wait for a slow client. Sensor
	// messages are snapshots, so when a client falls this far behind the oldest
	// waiting message is discarded rather than the broadcaster blocking.
	DefaultSendQueue = 16
)

// Hub fans messages out to WebSocket clients. Each client gets its own writer
// goroutine and bounded queue, so a client that stops reading delays nobody but
// itself and is dropped once a write to it times out.
type Hub struct {
	writeTimeout time.Duration
	pingInterval time.Duration
	pongWait     time.Duration
	sendQueue    int

	mu      sync.Mutex
	clients map[*hubClient]struct{}
	closed  bool
}

// hubClient is one connection and its pending messages.
type hubClient struct {
	conn      *websocket.Conn
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

// NewHub returns a hub using the default timeouts.
func NewHub() *Hub {
	return &Hub{
		writeTimeout: DefaultWriteTimeout,
		pingInterval: DefaultPingInterval,
		pongWait:     DefaultPongWait,
		sendQueue:    DefaultSendQueue,
		clients:      make(map[*hubClient]struct{}),
	}
}

// Serve registers an upgraded connection and blocks until the client goes away
// or the hub is closed. Incoming messages are read and discarded; reading is
// what processes pongs and notices a closed connection.
func (h *Hub) Serve(conn *websocket.Conn) {
	client := &hubClient{
		conn: conn,
		send: make(chan []byte, h.sendQueue),
		done: make(chan struct{}),
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		conn.Close()
		return
	}
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	go h.writeLoop(client)

	defer func() {
		h.mu.Lock()
		delete(h.clients, client)
		h.mu.Unlock()
		close(client.done)
		client.close()
	}()

	extend := func() { _ = conn.SetReadDeadline(time.Now().Add(h.pongWait)) }
	extend()
	conn.SetPongHandler(func(string) error {
		extend()
		return nil
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		extend()
	}
}

// writeLoop is the only goroutine that writes data frames to the client. A
// failed or timed-out write closes the connection, which ends the read loop in
// Serve and unregisters the client.
func (h *Hub) writeLoop(client *hubClient) {
	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-client.done:
			return
		case message := <-client.send:
			_ = client.conn.SetWriteDeadline(time.Now().Add(h.writeTimeout))
			if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				client.close()
				return
			}
		case <-ticker.C:
			deadline := time.Now().Add(h.writeTimeout)
			if err := client.conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				client.close()
				return
			}
		}
	}
}

// Broadcast queues message for every client without waiting on any of them.
func (h *Hub) Broadcast(message []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for client := range h.clients {
		client.enqueue(message)
	}
}

// enqueue adds a message, discarding the oldest waiting one when the queue is
// full. Broadcast holds the hub lock, so there is only ever one producer and the
// loop ends as soon as a slot is free.
func (c *hubClient) enqueue(message []byte) {
	for {
		select {
		case c.send <- message:
			return
		default:
		}
		select {
		case <-c.send:
		default:
		}
	}
}

// close shuts the connection once, however many paths ask for it.
func (c *hubClient) close() {
	c.closeOnce.Do(func() { _ = c.conn.Close() })
}

// Len returns the number of connected clients.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Close disconnects every client and refuses new ones.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.closed = true
	for client := range h.clients {
		client.close()
	}
}
