package main

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const maxRoomSize = 2

func allowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme == "wails" && host == "wails" {
		return true
	}
	if host == "localhost" || host == "wails.localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var roomPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{2,32}$`)
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return allowedOrigin(r.Header.Get("Origin"))
	},
}

type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
	room string
	name string
}

func (c *Client) writeJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
	return c.conn.WriteJSON(v)
}

var roomsMu sync.Mutex
var rooms = map[string]map[*Client]bool{}

func main() {
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"vcdesk-signaling"}`))
	})
	http.HandleFunc("/ws", handleWebSocket)

	addr := os.Getenv("VCDESK_LISTEN_ADDR")
	if addr == "" {
		// Hosted platforms such as Render provide PORT and require binding to
		// all interfaces. Local desktop builds remain loopback-only.
		if port := os.Getenv("PORT"); port != "" {
			addr = net.JoinHostPort("0.0.0.0", port)
		} else {
			addr = "127.0.0.1:8080"
		}
	}
	server := &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("VC Desk signaling server listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &Client{conn: conn}
	defer disconnect(client)
	conn.SetReadLimit(64 * 1024)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			_ = client.writeJSON(map[string]any{"type": "server-error", "message": "Invalid message format."})
			continue
		}
		typ, _ := msg["type"].(string)
		if typ == "join" {
			room, _ := msg["room"].(string)
			name, _ := msg["name"].(string)
			if !roomPattern.MatchString(room) {
				_ = client.writeJSON(map[string]any{"type": "server-error", "message": "Invalid room code."})
				return
			}
			if len(name) > 24 {
				name = name[:24]
			}
			if name == "" {
				name = "Guest"
			}
			if err := joinRoom(client, room, name); err != nil {
				_ = client.writeJSON(map[string]any{"type": "room-full"})
				return
			}
			continue
		}
		if client.room == "" {
			continue
		}
		if typ != "description" && typ != "candidate" {
			continue
		}
		forwardToRoom(client, msg)
	}
}

func joinRoom(client *Client, room, name string) error {
	roomsMu.Lock()
	defer roomsMu.Unlock()
	if client.room != "" {
		return errors.New("already joined")
	}
	members := rooms[room]
	if len(members) >= maxRoomSize {
		return errors.New("room full")
	}
	if members == nil {
		members = map[*Client]bool{}
		rooms[room] = members
	}
	client.room, client.name = room, name
	for peer := range members {
		if err := peer.writeJSON(map[string]any{"type": "peer-joined", "name": name}); err != nil {
			delete(members, peer)
		}
	}
	members[client] = true
	return nil
}

func forwardToRoom(sender *Client, msg map[string]any) {
	roomsMu.Lock()
	peers := make([]*Client, 0, len(rooms[sender.room]))
	for peer := range rooms[sender.room] {
		if peer != sender {
			peers = append(peers, peer)
		}
	}
	roomsMu.Unlock()
	for _, peer := range peers {
		if err := peer.writeJSON(msg); err != nil {
			disconnect(peer)
		}
	}
}

func disconnect(client *Client) {
	roomsMu.Lock()
	room := client.room
	if room != "" {
		members := rooms[room]
		delete(members, client)
		if len(members) == 0 {
			delete(rooms, room)
		}
	}
	client.room = ""
	roomsMu.Unlock()
	if room != "" {
		roomsMu.Lock()
		peers := make([]*Client, 0, len(rooms[room]))
		for peer := range rooms[room] {
			peers = append(peers, peer)
		}
		roomsMu.Unlock()
		for _, peer := range peers {
			_ = peer.writeJSON(map[string]any{"type": "peer-left"})
		}
	}
	_ = client.conn.Close()
}
