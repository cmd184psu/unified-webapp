package sshproxy

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/platform/middleware"
	"github.com/gorilla/websocket"
)

// clientMsg is a control frame sent by the browser as WebSocket text. Terminal
// keystrokes are sent as binary frames instead and never appear here.
//
// Password is the only route a password takes into the process: it travels in
// this JSON connect frame and never as a query parameter, a URL path segment,
// or a WebSocket subprotocol value. Its type is Secret so a %v or %+v dump of
// a clientMsg cannot print it (FR-N4).
type clientMsg struct {
	Type     string `json:"type"` // "connect" | "resize" | "disconnect"
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Key      string `json:"key"`
	Password Secret `json:"password"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

// serverMsg is a control frame sent to the browser as WebSocket text. Terminal
// output is sent as binary frames instead.
type serverMsg struct {
	Type    string `json:"type"`              // "status" | "error"
	State   string `json:"state,omitempty"`   // "connected" | "disconnected"
	Message string `json:"message,omitempty"` // populated for errors
}

// Handler upgrades a request to a WebSocket and bridges it to a single SSH PTY
// session. One Handler is shared by all terminals; each WebSocket gets its own
// independent SSH connection, so three browser panels use three sockets.
type Handler struct {
	dialer   Dialer
	sshDir   string
	upgrader websocket.Upgrader
}

// NewHandler builds a bridge that resolves selected key names against sshDir
// and dials with the given Dialer (pass SSHDialer{} in production).
func NewHandler(dialer Dialer, sshDir string) *Handler {
	return &Handler{
		dialer: dialer,
		sshDir: sshDir,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// Same-origin only: the SPA is served from this binary, so a
			// cross-origin WebSocket has no legitimate use here.
			CheckOrigin: sameOrigin,
		},
	}
}

// session holds the per-WebSocket mutable state, guarded so the read loop and
// the output pump can touch the active connection safely.
type session struct {
	ws *websocket.Conn

	mu   sync.Mutex
	conn Conn
	// cred is the password this session authenticated with, held only while the
	// connection is live. closeConn zeroes it, so a session that has ended holds
	// nothing (FR-N4).
	cred Secret
	// host and user identify the live connection in the disconnect audit line;
	// both are already client-supplied, non-secret connection metadata.
	host    string
	user    string
	writeMu sync.Mutex // serializes all writes to ws (gorilla allows one writer)
}

// ServeHTTP runs the bridge for one browser terminal until the socket closes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an error response.
		return
	}
	s := &session{ws: ws}
	defer s.closeConn()
	defer ws.Close()

	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		switch mt {
		case websocket.BinaryMessage:
			s.forwardInput(data)
		case websocket.TextMessage:
			var msg clientMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				s.sendError("malformed control message")
				continue
			}
			h.handleControl(s, msg)
		}
	}
}

func (h *Handler) handleControl(s *session, msg clientMsg) {
	switch msg.Type {
	case "connect":
		h.connect(s, msg)
	case "resize":
		s.resize(msg.Cols, msg.Rows)
	case "disconnect":
		s.closeConn()
		s.sendStatus("disconnected")
	default:
		s.sendError("unknown message type")
	}
}

// connect resolves the key, dials SSH, and starts the output pump. A failed
// attempt reports an error but leaves the socket open so the user can adjust
// the config and retry.
func (h *Handler) connect(s *session, msg clientMsg) {
	// Replace any prior connection on this socket.
	s.closeConn()

	// Exactly one of key or password, checked before anything is resolved or
	// dialed so a malformed frame never reaches the SSH layer.
	hasKey := strings.TrimSpace(msg.Key) != ""
	hasPassword := !msg.Password.IsZero()
	if hasKey == hasPassword {
		Auditf("connect host=%q user=%q outcome=rejected reason=credential", msg.Host, msg.User)
		s.sendError("provide exactly one of an SSH key or a password")
		return
	}

	var keyPath string
	if hasKey {
		var err error
		keyPath, err = ResolveKeyPath(h.sshDir, msg.Key)
		if err != nil {
			Auditf("connect host=%q user=%q auth=key outcome=rejected reason=key", msg.Host, msg.User)
			s.sendError("invalid SSH key selection")
			return
		}
	}

	params := ConnectParams{
		Host:     msg.Host,
		Port:     msg.Port,
		User:     msg.User,
		KeyPath:  keyPath,
		Password: msg.Password,
		Cols:     msg.Cols,
		Rows:     msg.Rows,
	}
	auth := authLabel(params)

	conn, err := h.dialer.Dial(params)
	if err != nil {
		Auditf("connect host=%q port=%d user=%q auth=%s outcome=failed", msg.Host, msg.Port, msg.User, auth)
		s.sendError(dialErrorMessage(err))
		return
	}

	s.mu.Lock()
	s.conn = conn
	s.cred = msg.Password
	s.host = msg.Host
	s.user = msg.User
	s.mu.Unlock()

	Auditf("connect host=%q port=%d user=%q auth=%s outcome=ok", msg.Host, msg.Port, msg.User, auth)
	s.sendStatus("connected")
	go s.pumpOutput(conn)
	go s.waitClose(conn)
}

// pumpOutput streams remote PTY bytes to the browser as binary frames until
// the connection ends.
func (s *session) pumpOutput(conn Conn) {
	buf := make([]byte, 4096)
	out := conn.Output()
	for {
		n, err := out.Read(buf)
		if n > 0 {
			if werr := s.writeBinary(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// waitClose reports a disconnect once the remote shell exits, but only if this
// connection is still the active one (guards against a newer reconnect).
func (s *session) waitClose(conn Conn) {
	_ = conn.Wait()
	s.mu.Lock()
	current := s.conn == conn
	host, user := s.host, s.user
	if current {
		s.conn = nil
		// The session is over; the password it authenticated with does not
		// outlive it (FR-N4).
		s.cred.Zero()
	}
	s.mu.Unlock()
	if current {
		Auditf("disconnect host=%q user=%q reason=remote", host, user)
		s.sendStatus("disconnected")
	}
}

func (s *session) forwardInput(data []byte) {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return
	}
	_, _ = conn.Write(data)
}

func (s *session) resize(cols, rows int) {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Resize(cols, rows)
	}
}

func (s *session) closeConn() {
	s.mu.Lock()
	conn := s.conn
	host, user := s.host, s.user
	s.conn = nil
	s.cred.Zero()
	s.mu.Unlock()
	if conn != nil {
		// Only the operator-initiated path gets here with a live connection;
		// a remote-initiated end is logged by waitClose instead, so the two
		// paths cannot double-log one disconnect.
		Auditf("disconnect host=%q user=%q reason=client", host, user)
		_ = conn.Close()
	}
}

// credential returns the password this session is currently holding. It exists
// for the FR-N4 lifetime test; nothing in production reads it.
func (s *session) credential() Secret {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cred
}

func (s *session) sendStatus(state string) {
	_ = s.writeJSON(serverMsg{Type: "status", State: state})
}

func (s *session) sendError(message string) {
	_ = s.writeJSON(serverMsg{Type: "error", Message: message})
}

func (s *session) writeJSON(v serverMsg) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return s.ws.WriteMessage(websocket.TextMessage, b)
}

func (s *session) writeBinary(data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return s.ws.WriteMessage(websocket.BinaryMessage, data)
}

// dialErrorMessage keeps client-facing text generic while logging detail for
// the operator. Remote/host specifics are not echoed to the browser.
func dialErrorMessage(err error) string {
	log.Printf("sshproxy: dial failed: %v", err)
	return "connection failed: check host, user, and key"
}

// sameOrigin wraps middleware.SameOrigin, adding the audit line the terminal
// bridge relies on when an upgrade is rejected.
var sameOrigin = func(r *http.Request) bool {
	if middleware.SameOrigin(r) {
		return true
	}
	// R1: a proxy that rewrites Host fails every upgrade here, and the pair is
	// the whole diagnosis -- log it rather than making the operator guess.
	Auditf("ws upgrade rejected origin=%q host=%q", r.Header.Get("Origin"), r.Host)
	return false
}
