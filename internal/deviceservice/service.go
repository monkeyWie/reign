// Package deviceservice provides the account-free device directory, pairing mailbox and relay.
package deviceservice

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/monkeyWie/reign/internal/connectivity"
	"github.com/monkeyWie/reign/internal/device"
	"github.com/monkeyWie/reign/internal/relay"
)

type Device struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connectionId"`
	Name         string `json:"name"`
	Public       string `json:"public"`
	TokenHash    string `json:"tokenHash,omitempty"`
	Host         bool   `json:"host"`
	Seen         int64  `json:"seen"`
}
type Pair struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Client   string `json:"client"`
	Name     string `json:"name"`
	Public   string `json:"public"`
	Envelope string `json:"envelope"`
	Reply    string `json:"reply,omitempty"`
	State    string `json:"state"`
	Expires  int64  `json:"expires"`
}
type state struct {
	ServiceID string            `json:"serviceId"`
	Devices   map[string]Device `json:"devices"`
	Grants    map[string]bool   `json:"grants"`
}
type bucket struct {
	At time.Time
	N  int
}
type Server struct {
	mu          sync.Mutex
	path        string
	state       state
	pairs       map[string]Pair
	rates       map[string]bucket
	relay       *relay.Server
	ServiceID   string
	Network     connectivity.Policy
	STUNPort    string
	TCPSTUNPort string
}

func hash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func New(path string, origins []string) (*Server, error) {
	s := &Server{path: path, state: state{Devices: map[string]Device{}, Grants: map[string]bool{}}, pairs: map[string]Pair{}, rates: map[string]bucket{}, Network: connectivity.DefaultPolicy()}
	if e := device.Load(path, &s.state); e != nil {
		if !isMissing(e) {
			return nil, e
		}
	}
	if s.state.Devices == nil {
		s.state.Devices = map[string]Device{}
	}
	if s.state.Grants == nil {
		s.state.Grants = map[string]bool{}
	}
	if s.state.ServiceID == "" {
		s.state.ServiceID = device.Random()
		if e := device.Save(path, s.state); e != nil {
			return nil, e
		}
	}
	s.ServiceID = s.state.ServiceID
	s.relay = relay.New("")
	s.relay.Origins = origins
	s.relay.Authorize = func(token, host, role string) (string, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		id := s.identity(token)
		if id == "" {
			return "", false
		}
		d := s.state.Devices[id]
		target, ok := s.state.Devices[host]
		if !ok {
			return "", false
		}
		if role == "host" {
			return id, id == host && d.Host
		}
		return id, role == "client" && target.Host && s.state.Grants[host+":"+id]
	}
	return s, nil
}
func (s *Server) identity(token string) string {
	if token == "" {
		return ""
	}
	h := hash(token)
	for id, d := range s.state.Devices {
		if d.TokenHash == h {
			return id
		}
	}
	return ""
}
func (s *Server) allow(k string, limit int) bool {
	now := time.Now()
	if len(s.rates) > 20000 {
		for key, v := range s.rates {
			if now.Sub(v.At) > time.Minute {
				delete(s.rates, key)
			}
		}
		if _, ok := s.rates[k]; !ok && len(s.rates) > 20000 {
			return false
		}
	}
	b := s.rates[k]
	if now.Sub(b.At) > time.Minute {
		b = bucket{At: now}
	}
	b.N++
	s.rates[k] = b
	return b.N <= limit
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/relay" {
		s.relay.ServeHTTP(w, r)
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		allowed := origin == "http://"+r.Host || origin == "https://"+r.Host
		for _, o := range s.relay.Origins {
			if match, _ := path.Match(o, origin); match {
				allowed = true
			}
		}
		if !allowed {
			http.Error(w, "origin denied", 403)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	}
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var in struct {
		Name, Public, ConnectionID, Host, ID, Envelope, Reply, Client string
		IsHost                                                        bool
		Allow                                                         bool
	}
	if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); e != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.allow("ip:"+ip, 300) {
		http.Error(w, "rate limited", 429)
		return
	}
	now := time.Now().Unix()
	for id, p := range s.pairs {
		if p.Expires < now {
			delete(s.pairs, id)
		}
	}
	write := func(v any) { w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(v) }
	if r.URL.Path == "/v2/network" {
		policy := s.Network
		if len(policy.SelfSTUN) == 0 {
			if endpoint := connectivity.STUNURL(r.Host, s.STUNPort); endpoint != "" {
				policy.SelfSTUN = []string{endpoint}
			}
		}
		if len(policy.TCPSTUN) == 0 && s.TCPSTUNPort != "" {
			if endpoint := connectivity.STUNURL(r.Host, s.TCPSTUNPort); endpoint != "" {
				policy.TCPSTUN = []string{strings.TrimPrefix(endpoint, "stun:")}
			}
		}
		write(policy)
		return
	}
	if r.URL.Path == "/v2/register" {
		if len(s.state.Devices) >= 10000 || !s.allow("register:"+ip, 10) {
			http.Error(w, "registration limit", 429)
			return
		}
		raw, e := hex.DecodeString(in.Public)
		if e != nil || len(raw) != 32 || len(in.Name) > 100 {
			http.Error(w, "invalid identity", 400)
			return
		}
		token := device.Random()
		id := relay.ID()
		var code string
		for {
			n, e := rand.Int(rand.Reader, big.NewInt(1_000_000))
			if e != nil {
				http.Error(w, "device ID allocation failed", 503)
				return
			}
			code = fmt.Sprintf("%06d", n.Int64())
			unique := true
			for _, d := range s.state.Devices {
				if d.ConnectionID == code {
					unique = false
					break
				}
			}
			if unique {
				break
			}
		}
		d := Device{ID: id, ConnectionID: code, Name: in.Name, Public: in.Public, TokenHash: hash(token), Host: in.IsHost, Seen: now}
		s.state.Devices[id] = d
		if e = device.Save(s.path, s.state); e != nil {
			delete(s.state.Devices, id)
			http.Error(w, "persistence failed", 500)
			return
		}
		write(map[string]any{"id": id, "connectionId": code, "token": token, "serviceId": s.ServiceID})
		return
	}
	id := s.identity(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if id == "" {
		http.Error(w, "unauthorized", 401)
		return
	}
	me := s.state.Devices[id]
	switch r.URL.Path {
	case "/v2/presence":
		if in.Name != "" && len(in.Name) <= 100 {
			me.Name = in.Name
		}
		me.Seen = now
		s.state.Devices[id] = me
		write(map[string]bool{"ok": true})
	case "/v2/resolve":
		code := strings.ReplaceAll(strings.ReplaceAll(in.ConnectionID, " ", ""), "-", "")
		for _, d := range s.state.Devices {
			if d.ConnectionID == code && d.Host {
				d.TokenHash = ""
				write(map[string]any{"device": d, "online": now-d.Seen < 45})
				return
			}
		}
		http.Error(w, "device not found", 404)
	case "/v2/pair/request":
		host, ok := s.state.Devices[in.Host]
		if !ok || !host.Host || len(in.Envelope) > 32000 || len(in.ID) != 32 {
			http.Error(w, "invalid pairing target", 400)
			return
		}
		if !s.allow("pair:"+id, 10) || !s.allow("hostpair:"+host.ID, 30) || len(s.pairs) > 1000 {
			http.Error(w, "pairing rate limited", 429)
			return
		}
		if _, exists := s.pairs[in.ID]; exists {
			http.Error(w, "duplicate request", 409)
			return
		}
		p := Pair{ID: in.ID, Host: host.ID, Client: id, Name: me.Name, Public: me.Public, Envelope: in.Envelope, State: "pending", Expires: now + 120}
		s.pairs[p.ID] = p
		write(p)
	case "/v2/pair/pending":
		out := []Pair{}
		for _, p := range s.pairs {
			if p.Host == id && p.State == "pending" {
				out = append(out, p)
			}
		}
		write(out)
	case "/v2/pair/result":
		p, ok := s.pairs[in.ID]
		if !ok || p.Client != id {
			http.Error(w, "pairing expired", 404)
			return
		}
		write(p)
	case "/v2/pair/respond":
		p, ok := s.pairs[in.ID]
		if !ok || p.Host != id || p.State != "pending" {
			http.Error(w, "request unavailable", 409)
			return
		}
		p.State = "denied"
		if in.Allow {
			if in.Reply == "" {
				http.Error(w, "missing encrypted reply", 400)
				return
			}
			s.state.Grants[p.Host+":"+p.Client] = true
			if e := device.Save(s.path, s.state); e != nil {
				delete(s.state.Grants, p.Host+":"+p.Client)
				http.Error(w, "persistence failed", 500)
				return
			}
			p.State = "approved"
			p.Reply = in.Reply
		}
		s.pairs[p.ID] = p
		write(p)
	case "/v2/revoke":
		s.relay.DisconnectDevice(id, in.Client)
		delete(s.state.Grants, id+":"+in.Client)
		if e := device.Save(s.path, s.state); e != nil {
			http.Error(w, "persistence failed", 500)
			return
		}
		write(map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}
