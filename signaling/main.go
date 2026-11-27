package main

import (
	"encoding/json"
	"bytes"
	"fmt"
	"io"
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

var turnRateMu sync.Mutex
var turnRequests = map[string][]time.Time{}

type cloudflareTURNResponse struct {
  IceServers struct {
    URLs []string `json:"urls"`
    Username string `json:"username"`
    Credential string `json:"credential"`
  } `json:"iceServers"`
}

func turnCredentials(w http.ResponseWriter, r *http.Request) {
  w.Header().Set("Access-Control-Allow-Origin", "*")
  w.Header().Set("Cache-Control", "no-store")
  w.Header().Set("Content-Type", "application/json")
  if r.Method != http.MethodGet { w.Header().Set("Allow", http.MethodGet); http.Error(w, "method not allowed", 405); return }
  if !allowTURNRequest(clientIP(r)) { http.Error(w, "rate limit exceeded", 429); return }
  keyID, secret := os.Getenv("CLOUDFLARE_TURN_KEY_ID"), os.Getenv("CLOUDFLARE_TURN_KEY_SECRET")
  if keyID == "" || secret == "" { http.Error(w, "TURN is not configured", 503); return }
  body, _ := json.Marshal(map[string]int{"ttl": 86400})
  endpoint := fmt.Sprintf("https://rtc.live.cloudflare.com/v1/turn/keys/%s/credentials/generate-ice-servers", keyID)
  req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
  if err != nil { http.Error(w, "could not create TURN request", 500); return }
  req.Header.Set("Authorization", "Bearer "+secret); req.Header.Set("Content-Type", "application/json")
  resp, err := (&http.Client{Timeout: 8*time.Second}).Do(req)
  if err != nil { http.Error(w, "TURN provider unavailable", 502); return }; defer resp.Body.Close()
  if resp.StatusCode < 200 || resp.StatusCode >= 300 { _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048)); http.Error(w, "TURN credential generation failed", 502); return }
  var result cloudflareTURNResponse
  if err := json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&result); err != nil || result.IceServers.Username == "" || result.IceServers.Credential == "" { http.Error(w, "invalid TURN provider response", 502); return }
  urls := make([]string, 0, len(result.IceServers.URLs)); for _, u := range result.IceServers.URLs { if !strings.Contains(u, ":53") { urls = append(urls, u) } }
  if len(urls) == 0 { http.Error(w, "no usable TURN URLs", 502); return }
  _ = json.NewEncoder(w).Encode(map[string]any{"turnAvailable": true, "iceServers": []any{map[string]any{"urls":"stun:stun.cloudflare.com:3478"}, map[string]any{"urls":urls,"username":result.IceServers.Username,"credential":result.IceServers.Credential}}})
}

func clientIP(r *http.Request) string { host, _, err := net.SplitHostPort(r.RemoteAddr); if err != nil { return r.RemoteAddr }; return host }
func allowTURNRequest(ip string) bool {
  now := time.Now(); turnRateMu.Lock(); defer turnRateMu.Unlock()
  recent := make([]time.Time, 0, len(turnRequests[ip])); for _, t := range turnRequests[ip] { if now.Sub(t) < time.Minute { recent = append(recent, t) } }
  if len(recent) >= 12 { turnRequests[ip] = recent; return false }; turnRequests[ip] = append(recent, now); return true
}

var roomsMu sync.Mutex
var rooms = map[string]map[*Client]bool{}

func main() {
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"vcdesk-signaling"}`))
	})
	http.HandleFunc("/ws", handleWebSocket)
	http.HandleFunc("/turn-credentials", turnCredentials)

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
