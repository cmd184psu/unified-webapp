// Package botchat connects agentflow to a BotChat service (rooms, callsigns, messages).
// Agentflow's own code is the only poster: it speaks for each stage under that stage's
// callsign, so no model ever holds the key. The key is read from a file on every call, so
// changing the setting or the key takes effect with no restart.
package botchat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client talks to one BotChat service.
type Client struct {
	// BaseURL is the service address, e.g. https://botchat.example.net.
	BaseURL string
	// KeyFile is the path of the file holding the shared API key; ~/ is the home directory.
	KeyFile string
	HTTP    *http.Client
}

// ErrNotFound is a 404 from the service (a room that is gone).
var ErrNotFound = errors.New("botchat: not found")

func (c *Client) key() (string, error) {
	p := c.KeyFile
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[2:])
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("botchat: read key file: %w", err)
	}
	k := strings.TrimSpace(string(b))
	if k == "" {
		return "", errors.New("botchat: key file is empty")
	}
	return k, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	key, err := c.key()
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := h.Do(req)
	if err != nil {
		return fmt.Errorf("botchat: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("botchat: %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

type room struct {
	ID     string `json:"id"`
	RoomID string `json:"roomId"`
	Name   string `json:"name"`
}

func (r room) id() string {
	if r.ID != "" {
		return r.ID
	}
	return r.RoomID
}

// Room returns the id of the room called name, creating it (as moderator `from`) when it does
// not exist yet. The listing and the create answer may wrap the room(s) in an object.
func (c *Client) Room(ctx context.Context, name, waveID, from string) (string, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/rooms", nil, &raw); err != nil {
		return "", err
	}
	var rooms []room
	if json.Unmarshal(raw, &rooms) != nil {
		var wrap struct {
			Rooms []room `json:"rooms"`
		}
		if err := json.Unmarshal(raw, &wrap); err != nil {
			return "", fmt.Errorf("botchat: cannot read the room list: %w", err)
		}
		rooms = wrap.Rooms
	}
	for _, r := range rooms {
		if r.Name == name && r.id() != "" {
			return r.id(), nil
		}
	}
	var made struct {
		room
		Room room `json:"room"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/rooms", map[string]string{"name": name, "waveId": waveID, "from": from}, &made); err != nil {
		return "", err
	}
	if id := made.id(); id != "" {
		return id, nil
	}
	if id := made.Room.id(); id != "" {
		return id, nil
	}
	return "", errors.New("botchat: the new room came back without an id")
}

// Post sends one message to a room. `to` is a callsign or "everyone".
func (c *Client) Post(ctx context.Context, roomID, from, to, content string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/rooms/"+roomID+"/messages", map[string]string{"from": from, "to": to, "content": content}, nil)
}
