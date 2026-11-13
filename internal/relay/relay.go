// Package relay routes opaque encrypted packets. It never receives host pairing keys.
package relay

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Frame struct {
	Peer   string `json:"peer,omitempty"`
	Device string `json:"device,omitempty"`
	Kind   string `json:"kind"`
	Body   []byte `json:"body,omitempty"`
}
type connection struct {
	ws     *websocket.Conn
	device string
}

func (c *connection) send(f Frame) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	b, _ := json.Marshal(f)
	return c.ws.Write(ctx, websocket.MessageText, b)
}

type relayRoom struct {
	host    *connection
	clients map[string]*connection
}
type Server struct {
	token            string
	mu               sync.Mutex
	rooms            map[string]*relayRoom
	Authorize        func(credential, host, role string) (string, bool)
	AuthorizeContext func(context.Context, string, string, string) (string, bool)
	Origins          []string
}

func New(token string) *Server { return &Server{token: token, rooms: make(map[string]*relayRoom)} }
func ID() string               { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/relay" {
		http.NotFound(w, r)
		return
	}
	credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if credential == "" {
		protocols := strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",")
		for i := range protocols {
			protocols[i] = strings.TrimSpace(protocols[i])
		}
		version := false
		for _, p := range protocols {
			if p == "agenthub.v1" {
				version = true
			}
		}
		if version {
			for _, p := range protocols {
				if strings.HasPrefix(p, "auth.") {
					if b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(p, "auth.")); err == nil {
						credential = string(b)
					}
				}
			}
		}
	}
	deviceID := ""
	authorized := s.token != "" && subtle.ConstantTimeCompare([]byte(credential), []byte(s.token)) == 1
	if s.Authorize != nil {
		deviceID, authorized = s.Authorize(credential, r.URL.Query().Get("id"), r.URL.Query().Get("role"))
	}
	if s.AuthorizeContext != nil {
		deviceID, authorized = s.AuthorizeContext(r.Context(), credential, r.URL.Query().Get("id"), r.URL.Query().Get("role"))
	}
	if !authorized {
		http.Error(w, "unauthorized", 401)
		return
	}
	if r.Header.Get("Origin") != "" && r.URL.Query().Get("role") != "client" {
		http.Error(w, "browser host not allowed", 403)
		return
	}
	id, role := r.URL.Query().Get("id"), r.URL.Query().Get("role")
	if len(id) < 16 || len(id) > 128 || (role != "host" && role != "client") {
		http.Error(w, "invalid route", 400)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"agenthub.v1"}, OriginPatterns: s.Origins})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(2 << 20)
	c := &connection{ws: ws, device: deviceID}
	peer := ID()
	s.mu.Lock()
	room := s.rooms[id]
	if role == "host" {
		if room != nil {
			s.mu.Unlock()
			_ = ws.Close(websocket.StatusPolicyViolation, "host already connected")
			return
		}
		room = &relayRoom{host: c, clients: make(map[string]*connection)}
		s.rooms[id] = room
	} else {
		if room == nil || len(room.clients) >= 16 {
			s.mu.Unlock()
			_ = ws.Close(websocket.StatusTryAgainLater, "host unavailable or full")
			return
		}
		room.clients[peer] = c
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if role == "host" {
			delete(s.rooms, id)
		} else {
			delete(room.clients, peer)
		}
		clients := make([]*connection, 0, len(room.clients))
		for _, v := range room.clients {
			clients = append(clients, v)
		}
		s.mu.Unlock()
		if role == "host" {
			for _, v := range clients {
				_ = v.ws.CloseNow()
			}
		} else {
			_ = room.host.send(Frame{Peer: peer, Kind: "bye"})
		}
	}()
	if err = c.send(Frame{Peer: peer, Kind: "welcome"}); err != nil {
		return
	}
	// Ping bounds half-open connections and keeps common reverse proxies alive.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				authorized := true
				if s.AuthorizeContext != nil {
					_, authorized = s.AuthorizeContext(ctx, credential, id, role)
				} else if s.Authorize != nil {
					_, authorized = s.Authorize(credential, id, role)
				}
				if !authorized {
					_ = ws.Close(websocket.StatusPolicyViolation, "authorization expired or revoked")
					return
				}
				pc, stop := context.WithTimeout(ctx, 10*time.Second)
				err := ws.Ping(pc)
				stop()
				if err != nil {
					_ = ws.CloseNow()
					return
				}
			}
		}
	}()
	for {
		_, raw, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var f Frame
		if json.Unmarshal(raw, &f) != nil {
			return
		}
		if f.Kind != "data" && f.Kind != "signal" && f.Kind != "chunk" && f.Kind != "hello" {
			return
		}
		s.mu.Lock()
		target := room.host
		if role == "host" {
			target = room.clients[f.Peer]
			f.Device = ""
		} else {
			f.Peer = peer
			f.Device = deviceID
		}
		s.mu.Unlock()
		if target != nil {
			if target.send(f) != nil {
				return
			}
		}
	}
}

// DisconnectDevice closes only the revoked controller's signaling and relay links.
func (s *Server) DisconnectDevice(host, device string) {
	s.mu.Lock()
	var sockets []*websocket.Conn
	if room := s.rooms[host]; room != nil {
		for _, c := range room.clients {
			if c.device == device {
				sockets = append(sockets, c.ws)
			}
		}
	}
	s.mu.Unlock()
	for _, ws := range sockets {
		_ = ws.CloseNow()
	}
}
