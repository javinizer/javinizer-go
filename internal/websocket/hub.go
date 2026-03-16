package websocket

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// ProgressMessage represents a progress update message
type ProgressMessage struct {
	JobID     string  `json:"job_id"`
	FileIndex int     `json:"file_index"`
	FilePath  string  `json:"file_path"`
	Status    string  `json:"status"`
	Progress  float64 `json:"progress"`
	Message   string  `json:"message"`
	Error     string  `json:"error,omitempty"`
}

// Client represents a websocket client
type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	done       chan struct{}
	closeOnce  sync.Once
	mu         sync.RWMutex
}

// NewHub creates a new Hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		done:       make(chan struct{}),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.closeDone()

			// Context cancelled, clean up all clients
			// Snapshot clients first to minimize lock duration
			h.mu.Lock()
			clients := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				clients = append(clients, client)
			}
			h.clients = make(map[*Client]bool)
			h.mu.Unlock()

			// Clean up clients without holding lock
			for _, client := range clients {
				close(client.send)
			}
			logging.Infof("WebSocket hub stopped")
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			logging.Infof("WebSocket client connected. Total clients: %d", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			logging.Infof("WebSocket client disconnected. Total clients: %d", len(h.clients))

		case message := <-h.broadcast:
			// Phase 1: Collect clients under read lock (prevents deadlock during channel sends)
			h.mu.RLock()
			clients := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				clients = append(clients, client)
			}
			h.mu.RUnlock()

			// Phase 2: Send to clients without holding lock (channel sends can block)
			var toRemove []*Client
			for _, client := range clients {
				select {
				case client.send <- message:
					// Message sent successfully
				default:
					// Client's send channel is full, mark for removal
					close(client.send)
					toRemove = append(toRemove, client)
				}
			}

			// Phase 3: Remove disconnected clients with brief write lock
			if len(toRemove) > 0 {
				h.mu.Lock()
				for _, client := range toRemove {
					delete(h.clients, client)
				}
				h.mu.Unlock()
			}
		}
	}
}

func (h *Hub) closeDone() {
	h.closeOnce.Do(func() {
		close(h.done)
	})
}

// Register registers a new client
func (h *Hub) Register(client *Client) {
	if h == nil || client == nil {
		return
	}
	select {
	case <-h.done:
		return
	case h.register <- client:
	}
}

// Unregister unregisters a client
func (h *Hub) Unregister(client *Client) {
	if h == nil || client == nil {
		return
	}
	select {
	case <-h.done:
		return
	case h.unregister <- client:
	}
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(message interface{}) error {
	// Handle nil hub (can occur during cleanup in tests when hub is being replaced)
	if h == nil {
		return nil // Silently ignore broadcasts to nil hub
	}

	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	// Use select with default to avoid blocking if hub is shutting down
	select {
	case h.broadcast <- data:
		return nil
	default:
		// Hub is busy or shutting down, drop the message to avoid blocking
		return nil
	}
}

// BroadcastProgress sends a progress update to all clients
func (h *Hub) BroadcastProgress(msg *ProgressMessage) error {
	return h.Broadcast(msg)
}

// NewClient creates a new client
func NewClient(conn *websocket.Conn) *Client {
	return &Client{
		conn: conn,
		send: make(chan []byte, 256),
	}
}

// WritePump pumps messages from the hub to the websocket connection
func (c *Client) WritePump() {
	defer func() {
		_ = c.conn.Close()
	}()

	for message := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			logging.Errorf("Error writing to websocket: %v", err)
			return
		}
	}
	// The hub closed the channel.
	_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
}

// ReadPump pumps messages from the websocket connection to the hub
func (c *Client) ReadPump(hub *Hub) {
	defer func() {
		hub.Unregister(c)
		_ = c.conn.Close()
	}()

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logging.Errorf("Unexpected websocket error: %v", err)
			}
			break
		}
		// We don't process client messages for now, just keep the connection alive
	}
}
