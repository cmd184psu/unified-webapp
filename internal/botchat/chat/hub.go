package chat

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	sendBuffer = 32
)

type wsEnvelope struct {
	Type    string   `json:"type"`
	RoomID  string   `json:"roomId,omitempty"`
	Message *Message `json:"message,omitempty"`
}

type client struct {
	roomID string
	conn   *websocket.Conn
	send   chan []byte
}

type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*client]struct{})}
}

func (h *Hub) Register(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *Hub) Unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
}

func (h *Hub) Broadcast(roomID string, msg Message) {
	env := wsEnvelope{Type: "message", RoomID: roomID, Message: &msg}
	b, err := json.Marshal(env)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.roomID != roomID {
			continue
		}
		select {
		case c.send <- b:
		default:
		}
	}
}

func (h *Hub) BroadcastRoomDeleted(roomID string) {
	env := wsEnvelope{Type: "room_deleted", RoomID: roomID}
	b, err := json.Marshal(env)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.roomID != roomID {
			continue
		}
		select {
		case c.send <- b:
		default:
		}
	}
}

func (h *Hub) ServeWS(store *Store, roomID string, conn *websocket.Conn) {
	c := &client{roomID: roomID, conn: conn, send: make(chan []byte, sendBuffer)}
	h.Register(c)

	msgs, err := store.GetMessages(roomID)
	if err == nil {
		for _, m := range msgs {
			env := wsEnvelope{Type: "message", RoomID: roomID, Message: &m}
			if b, err := json.Marshal(env); err == nil {
				select {
				case c.send <- b:
				default:
				}
			}
		}
	}

	go writePump(h, c)
	readPump(h, c)
}

func writePump(h *Hub, c *client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func readPump(h *Hub, c *client) {
	defer func() {
		h.Unregister(c)
		_ = c.conn.Close()
	}()
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("chat: websocket read error: %v", err)
			}
			return
		}
	}
}
